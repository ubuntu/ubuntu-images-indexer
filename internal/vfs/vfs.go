// Package vfs exposes the indexed object store as read-only fs.FS trees so
// they can be served over protocols such as rsync. Every tree is a projection
// of the same canonical tree onto a path layout: the canonical layout itself
// (FS) or one of the legacy layouts (View). Directories appear as
// fs.ReadDirFile; file contents stream from the storage backend on demand.
package vfs

import (
	"context"
	"io/fs"
	"strings"

	"github.com/ubuntu/ubimi/internal/images"
	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

// pathMapper resolves the paths of one projection of the canonical tree: the
// canonical mapping (canonicalMapper) or a legacy layout projection (View).
type pathMapper interface {
	// resolve reports the path's kind: an indexed object (obj set) or a
	// directory. ok is false when the path does not exist.
	resolve(name string) (dir bool, obj canonicalObject, ok bool)
	// entries lists the directory entries at name. It is only called for
	// directories, including "." for the root.
	entries(name string) []fs.DirEntry
}

// fsView is the single fs.FS implementation behind every projection: it turns
// a pathMapper's paths into files whose contents stream from storage.
//
// ctx is held here because fs.FS has no context parameter: streams opened by
// Open are bound to it so shutdown cancels in-flight transfers.
type fsView struct {
	ctx context.Context
	st  storage.Storage
	m   pathMapper
}

// Open implements fs.FS.
func (f fsView) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, notExist(name)
	}
	if name == "." {
		return &dirFile{src: f.m, name: "."}, nil
	}
	dir, obj, ok := f.m.resolve(name)
	if !ok {
		return nil, notExist(name)
	}
	if dir {
		return &dirFile{src: f.m, name: name}, nil
	}
	return &objectFile{
		ctx:       f.ctx,
		st:        f.st,
		name:      name,
		container: obj.container,
		object:    obj.object.Name,
		info:      objectInfo(base(name), obj.object),
	}, nil
}

// canonicalObject pairs a canonical object with the storage container it
// lives in, so its bytes can be streamed on demand. Layout views also keep
// the parsed release to retain the newest one on a path clash.
type canonicalObject struct {
	container string
	object    storage.Object
	release   images.Release
}

// FS is a read-only fs.FS view of the indexed object store in its canonical
// layout: containers appear as top-level directories and object names as
// nested paths. It backs rsync modules that name no layout.
type FS struct {
	fsView
}

// New returns an FS reading structure from idx and contents from st. Streams
// are opened with ctx so that shutdown cancels in-flight transfers.
func New(ctx context.Context, idx *index.Index, st storage.Storage) *FS {
	return &FS{fsView{ctx: ctx, st: st, m: canonicalMapper{idx: idx}}}
}

// canonicalMapper maps paths in the canonical layout: the first path segment
// selects the container and the remainder is the object prefix within it.
type canonicalMapper struct {
	idx *index.Index
}

// resolve implements pathMapper.
func (c canonicalMapper) resolve(name string) (dir bool, obj canonicalObject, ok bool) {
	container, prefix, _ := strings.Cut(name, "/")
	if prefix == "" {
		// A container root is a directory whenever the container exists,
		// even if it currently holds no objects.
		if !c.idx.HasContainer(container) {
			return false, canonicalObject{}, false
		}
		return true, canonicalObject{}, true
	}
	if obj, ok := c.idx.Lookup(container, prefix); ok {
		return false, canonicalObject{container: container, object: obj}, true
	}
	files, dirs := c.idx.Children(container, prefix)
	return true, canonicalObject{}, len(files) > 0 || len(dirs) > 0
}

// entries implements pathMapper.
func (c canonicalMapper) entries(name string) []fs.DirEntry {
	if name == "." {
		var out []fs.DirEntry
		for _, c := range c.idx.Containers() {
			out = append(out, dirEntry{name: c, dir: true})
		}
		return out
	}
	container, prefix, _ := strings.Cut(name, "/")
	files, dirs := c.idx.Children(container, prefix)
	out := make([]fs.DirEntry, 0, len(dirs)+len(files))
	for _, d := range dirs {
		out = append(out, dirEntry{name: d, dir: true})
	}
	for _, obj := range files {
		out = append(out, dirEntry{name: base(obj.Name), info: objectInfo(base(obj.Name), obj)})
	}
	return out
}

// Views holds one View per known layout, all sharing a single semantic model
// of the canonical tree. The HTTP listener and the rsync daemon pick their
// filesystems from here, so a layout is materialised once per index snapshot
// no matter how many listeners serve it.
type Views map[Layout]*View

// NewViews returns a View for every known layout, reading structure from idx
// and contents from st. Streams are opened with ctx so that shutdown cancels
// in-flight transfers.
func NewViews(ctx context.Context, idx *index.Index, st storage.Storage) Views {
	cat := NewCatalog(idx)
	views := make(Views, len(Layouts()))
	for _, l := range Layouts() {
		views[l] = cat.View(ctx, st, l)
	}
	return views
}
