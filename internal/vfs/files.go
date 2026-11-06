package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/ubuntu/ubimi/internal/storage"
)

// dirFile is a directory: an fs.File and fs.ReadDirFile backed by the
// entries of its pathMapper.
type dirFile struct {
	src     pathMapper
	name    string
	entries []fs.DirEntry
	offset  int
}

func (d *dirFile) Stat() (fs.FileInfo, error) { return dirInfo(base(d.name)), nil }
func (d *dirFile) Close() error               { return nil }
func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fmt.Errorf("is a directory")}
}

func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.entries == nil {
		d.entries = d.src.entries(d.name)
	}
	if n <= 0 {
		entries := d.entries[d.offset:]
		d.offset = len(d.entries)
		return entries, nil
	}
	if d.offset >= len(d.entries) {
		return nil, io.EOF
	}
	end := min(d.offset+n, len(d.entries))
	entries := d.entries[d.offset:end]
	d.offset = end
	return entries, nil
}

// objectFile is a regular file whose contents are streamed from storage. It
// implements io.Seeker, which rsync requires, by re-opening a range request.
type objectFile struct {
	ctx       context.Context
	st        storage.Storage
	name      string
	container string
	object    string
	info      fs.FileInfo
	rc        io.ReadCloser
	offset    int64
	eof       bool
}

func (o *objectFile) Stat() (fs.FileInfo, error) { return o.info, nil }

func (o *objectFile) Read(p []byte) (int, error) {
	if o.eof {
		return 0, io.EOF
	}
	if o.rc == nil {
		rc, err := o.st.Open(o.ctx, o.container, o.object, o.offset)
		if err != nil {
			return 0, &fs.PathError{Op: "read", Path: o.name, Err: err}
		}
		o.rc = rc
	}
	n, err := o.rc.Read(p)
	o.offset += int64(n)
	if err == io.EOF {
		// The Swift reader may return the final bytes together with io.EOF.
		// rsync's sender drops n when it sees io.EOF, so return the data with
		// a nil error and report EOF on the next call instead.
		o.eof = true
		if n > 0 {
			return n, nil
		}
		return 0, io.EOF
	}
	return n, err
}

func (o *objectFile) Close() error {
	if o.rc != nil {
		return o.rc.Close()
	}
	return nil
}

func (o *objectFile) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = o.offset + offset
	case io.SeekEnd:
		abs = o.info.Size() + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative position")
	}
	if abs != o.offset {
		if o.rc != nil {
			o.rc.Close()
			o.rc = nil
		}
		o.offset = abs
		o.eof = false
	}
	return abs, nil
}

// dirEntry is a minimal fs.DirEntry.
type dirEntry struct {
	name string
	dir  bool
	info fs.FileInfo
}

func (e dirEntry) Name() string               { return e.name }
func (e dirEntry) IsDir() bool                { return e.dir }
func (e dirEntry) Type() fs.FileMode          { return dirInfo(e.name).Mode().Type() }
func (e dirEntry) Info() (fs.FileInfo, error) { return e.infoOrDirInfo(), nil }
func (e dirEntry) infoOrDirInfo() fs.FileInfo {
	if e.info != nil {
		return e.info
	}
	return dirInfo(e.name)
}

// fileInfo is an fs.FileInfo for both directories and objects.
type fileInfo struct {
	name string
	size int64
	mode fs.FileMode
	mod  time.Time
}

func (i fileInfo) Name() string       { return i.name }
func (i fileInfo) Size() int64        { return i.size }
func (i fileInfo) Mode() fs.FileMode  { return i.mode }
func (i fileInfo) ModTime() time.Time { return i.mod }
func (i fileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i fileInfo) Sys() any           { return nil }

func dirInfo(name string) fileInfo {
	return fileInfo{name: name, mode: fs.ModeDir | 0o555}
}

func objectInfo(name string, obj storage.Object) fileInfo {
	return fileInfo{name: name, size: obj.Size, mode: 0o444, mod: obj.LastModified}
}

// base returns the last path element of a slash-separated name.
func base(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func notExist(name string) error {
	return &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}
