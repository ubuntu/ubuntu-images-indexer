// Package vfs exposes the indexed object store as a read-only fs.FS so it can
// be served over protocols such as rsync. Containers appear as top-level
// directories and object names as nested paths; file contents are streamed
// from the storage backend on demand.
package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

// FS is a read-only fs.FS view of the indexed object store.
type FS struct {
	ctx context.Context
	idx *index.Index
	st  storage.Storage
}

// New returns an FS reading structure from idx and contents from st. Streams
// are opened with ctx so that shutdown cancels in-flight transfers.
func New(ctx context.Context, idx *index.Index, st storage.Storage) *FS {
	return &FS{ctx: ctx, idx: idx, st: st}
}

// Open implements fs.FS.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, notExist(name)
	}
	if name == "." {
		return &dirFile{fs: f, name: "."}, nil
	}

	container, prefix, _ := strings.Cut(name, "/")
	if !f.idx.HasContainer(container) {
		return nil, notExist(name)
	}
	if prefix == "" {
		return &dirFile{fs: f, name: name}, nil
	}
	if obj, ok := f.idx.Lookup(container, prefix); ok {
		return &objectFile{
			fs:        f,
			name:      name,
			container: container,
			object:    prefix,
			info:      objectInfo(base(prefix), obj),
		}, nil
	}
	files, dirs := f.idx.Children(container, prefix)
	if len(files) == 0 && len(dirs) == 0 {
		return nil, notExist(name)
	}
	return &dirFile{fs: f, name: name}, nil
}

// entries returns the directory entries for name.
func (f *FS) entries(name string) []fs.DirEntry {
	if name == "." {
		var out []fs.DirEntry
		for _, c := range f.idx.Containers() {
			out = append(out, dirEntry{name: c, dir: true})
		}
		return out
	}
	container, prefix, _ := strings.Cut(name, "/")
	files, dirs := f.idx.Children(container, prefix)
	out := make([]fs.DirEntry, 0, len(dirs)+len(files))
	for _, d := range dirs {
		out = append(out, dirEntry{name: d, dir: true})
	}
	for _, obj := range files {
		out = append(out, dirEntry{name: base(obj.Name), info: objectInfo(base(obj.Name), obj)})
	}
	return out
}

// dirFile is a directory: an fs.File and fs.ReadDirFile.
type dirFile struct {
	fs      *FS
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
		d.entries = d.fs.entries(d.name)
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
	fs        *FS
	name      string
	container string
	object    string
	info      fs.FileInfo
	rc        io.ReadCloser
	offset    int64
}

func (o *objectFile) Stat() (fs.FileInfo, error) { return o.info, nil }

func (o *objectFile) Read(p []byte) (int, error) {
	if o.rc == nil {
		rc, err := o.fs.st.Open(o.fs.ctx, o.container, o.object, o.offset)
		if err != nil {
			return 0, &fs.PathError{Op: "read", Path: o.name, Err: err}
		}
		o.rc = rc
	}
	n, err := o.rc.Read(p)
	o.offset += int64(n)
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
