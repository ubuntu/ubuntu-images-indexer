package vfs

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

// Layout identifies a legacy tree layout exposed over rsync. The object store
// itself keeps the new canonical layout, stored in the images container as
// {series}/{product}/{path}.
type Layout string

const (
	// LayoutImages exposes the canonical images tree unchanged.
	LayoutImages Layout = "images"
	// LayoutCDImage exposes the old cdimage.ubuntu.com tree.
	LayoutCDImage Layout = "cdimage"
	// LayoutReleases exposes the old releases.ubuntu.com tree.
	LayoutReleases Layout = "releases"

	// CanonicalContainer is the storage container holding the new tree.
	CanonicalContainer = "images"
)

// ParseLayout returns the layout with the given module name.
func ParseLayout(name string) (Layout, bool) {
	switch l := Layout(name); l {
	case LayoutImages, LayoutCDImage, LayoutReleases:
		return l, true
	default:
		return "", false
	}
}

// canonical translates a path in this layout to an object in the canonical
// images container tree.
func (l Layout) canonical(name string) (string, bool) {
	if name == "" || name == "." || !fs.ValidPath(name) {
		return "", false
	}
	if l == LayoutImages {
		return name, true
	}
	if !strings.Contains(name, "/") {
		return name, true // shared root files such as HEADER.html
	}

	parts := strings.Split(name, "/")
	switch l {
	case LayoutReleases:
		series, rest, _ := strings.Cut(name, "/")
		if strings.HasPrefix(rest, "netboot/") {
			return series + "/netboot/" + strings.TrimPrefix(rest, "netboot/"), true
		}
		return series + "/ubuntu/" + rest, true

	case LayoutCDImage:
		if parts[0] == "netboot" && len(parts) >= 3 {
			return parts[1] + "/netboot/" + strings.Join(parts[2:], "/"), true
		}
		if parts[0] == "ubuntu-core" && len(parts) >= 5 {
			series := parts[1]
			if !strings.Contains(series, ".") {
				series += ".04"
			}
			return series + "/ubuntu-core/" + strings.Join(parts[4:], "/"), true
		}
		if len(parts) >= 5 && strings.HasPrefix(parts[2], "daily-") {
			return parts[1] + "/" + parts[0] + "/" + strings.Join(parts[4:], "/"), true
		}
		if len(parts) >= 5 && parts[1] == "releases" && parts[3] == "release" {
			return parts[2] + "/" + parts[0] + "/" + strings.Join(parts[4:], "/"), true
		}
		if len(parts) >= 4 && parts[1] == "releases" {
			return parts[2] + "/" + parts[0] + "/" + strings.Join(parts[3:], "/"), true
		}
	}
	return "", false
}

// virtual translates a canonical object name to the paths this layout exposes.
func (l Layout) virtual(name string) []string {
	if name == "" || name == "." || !fs.ValidPath(name) {
		return nil
	}
	if l == LayoutImages {
		return []string{name}
	}
	if !strings.Contains(name, "/") {
		return []string{name} // shared root files
	}

	series, rest, _ := strings.Cut(name, "/")
	product, sub, ok := strings.Cut(rest, "/")
	if !ok {
		return nil
	}

	switch l {
	case LayoutReleases:
		switch product {
		case "ubuntu":
			return []string{series + "/" + sub}
		case "netboot":
			return []string{series + "/netboot/" + sub}
		}
	case LayoutCDImage:
		switch product {
		case "ubuntu-core":
			major, _, _ := strings.Cut(series, ".")
			return []string{"ubuntu-core/" + major + "/stable/current/" + sub}
		case "ubuntu-server":
			channel := "daily-live"
			if strings.Contains(sub, "preinstalled") {
				channel = "daily-preinstalled"
			}
			return []string{"ubuntu-server/" + series + "/" + channel + "/current/" + sub}
		case "ubuntu-wsl":
			return []string{"ubuntu-wsl/" + series + "/daily-live/current/" + sub}
		case "netboot":
			return []string{"netboot/" + series + "/" + sub}
		default:
			return []string{product + "/releases/" + series + "/release/" + sub}
		}
	}
	return nil
}

// View is a read-only fs.FS exposing the canonical images container through a
// legacy layout.
type View struct {
	ctx    context.Context
	idx    *index.Index
	st     storage.Storage
	layout Layout

	mu      sync.RWMutex
	version uint64
	loaded  bool
	objects map[string]storage.Object
	names   []string
}

// NewLayout returns a view of the canonical images container through layout l.
func NewLayout(ctx context.Context, idx *index.Index, st storage.Storage, l Layout) *View {
	return &View{ctx: ctx, idx: idx, st: st, layout: l}
}

// Open implements fs.FS.
func (v *View) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, notExist(name)
	}
	if name == "." {
		return &dirFile{src: v, name: "."}, nil
	}

	v.mu.Lock()
	v.ensureLocked()
	obj, ok := v.objects[name]
	if !ok {
		if canonical, found := v.layout.canonical(name); found {
			obj, ok = v.idx.Lookup(CanonicalContainer, canonical)
		}
	}
	files, dirs := v.childrenLocked(name)
	v.mu.Unlock()

	if ok {
		return &objectFile{
			ctx:       v.ctx,
			st:        v.st,
			name:      name,
			container: CanonicalContainer,
			object:    obj.Name,
			info:      objectInfo(base(name), obj),
		}, nil
	}
	if len(files) == 0 && len(dirs) == 0 {
		return nil, notExist(name)
	}
	return &dirFile{src: v, name: name}, nil
}

func (v *View) entries(name string) []fs.DirEntry {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()
	return v.entriesLocked(name)
}

func (v *View) ensureLocked() {
	version := v.idx.Version()
	if v.loaded && v.version == version {
		return
	}
	objects := make(map[string]storage.Object)
	for _, obj := range v.idx.Objects(CanonicalContainer) {
		for _, name := range v.layout.virtual(obj.Name) {
			if name == "" || !fs.ValidPath(name) {
				continue
			}
			objects[name] = obj
		}
	}
	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
	}
	slices.Sort(names)
	v.objects = objects
	v.names = names
	v.version = version
	v.loaded = true
}

type virtualObject struct {
	name string
	obj  storage.Object
}

func (v *View) childrenLocked(name string) (files []virtualObject, dirs []string) {
	prefix := strings.Trim(name, "/")
	if prefix != "" {
		prefix += "/"
	}
	start, _ := slices.BinarySearch(v.names, prefix)
	for _, n := range v.names[start:] {
		rest, ok := strings.CutPrefix(n, prefix)
		if !ok {
			break
		}
		if rest == "" {
			continue
		}
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			if dir := rest[:i]; len(dirs) == 0 || dirs[len(dirs)-1] != dir {
				dirs = append(dirs, dir)
			}
			continue
		}
		files = append(files, virtualObject{name: n, obj: v.objects[n]})
	}
	return files, dirs
}

func (v *View) entriesLocked(name string) []fs.DirEntry {
	files, dirs := v.childrenLocked(name)
	out := make([]fs.DirEntry, 0, len(dirs)+len(files))
	for _, d := range dirs {
		out = append(out, dirEntry{name: d, dir: true})
	}
	for _, f := range files {
		out = append(out, dirEntry{name: base(f.name), info: objectInfo(base(f.name), f.obj)})
	}
	return out
}
