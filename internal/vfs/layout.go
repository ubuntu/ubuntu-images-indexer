package vfs

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/ubuntu/ubimi/internal/images"
	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

// Layout identifies a legacy tree layout exposed over rsync. The object store
// itself keeps the new canonical layout, spread across containers as
// {series}/{product}/{path} object names.
type Layout string

const (
	// LayoutImages exposes the canonical images tree unchanged.
	LayoutImages Layout = "images"
	// LayoutCDImage exposes the old cdimage.ubuntu.com tree.
	LayoutCDImage Layout = "cdimage"
	// LayoutReleases exposes the old releases.ubuntu.com tree.
	LayoutReleases Layout = "releases"
)

// legacyContainers hold the old tree layouts (cdimage.ubuntu.com and
// releases.ubuntu.com). Their objects are not part of the canonical images
// tree and must not be translated into it.
var legacyContainers = map[string]bool{
	string(LayoutCDImage):  true,
	string(LayoutReleases): true,
}

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

// virtual translates a canonical object name in container to the paths this
// layout exposes. The images layout keeps the canonical tree unchanged, but
// prefixed with the object's per-series container so that the containers stay
// visible.
func (l Layout) virtual(container, name string) []string {
	if name == "" || name == "." || !fs.ValidPath(name) {
		return nil
	}
	if l == LayoutImages {
		return []string{container + "/" + name}
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

// canonicalObject pairs a canonical object with the storage container it
// lives in, so its bytes can be streamed on demand.
type canonicalObject struct {
	container string
	object    storage.Object
}

// View is a read-only fs.FS exposing the canonical images tree through a
// legacy layout. The canonical tree is spread across the object store's
// containers: each object's name is {series}/{product}/{path}.
type View struct {
	ctx    context.Context
	idx    *index.Index
	st     storage.Storage
	layout Layout

	mu       sync.RWMutex
	version  uint64
	loaded   bool
	objects  map[string]canonicalObject // virtual path -> object
	byObject map[string]canonicalObject // canonical object name -> object
	names    []string
	catalog  *images.Catalog // series/products/latest discovered from the tree
}

// NewLayout returns a view of the canonical images tree through layout l.
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
	obj, ok := v.lookupLocked(name)
	files, dirs := v.childrenLocked(name)
	v.mu.Unlock()

	if ok {
		return &objectFile{
			ctx:       v.ctx,
			st:        v.st,
			name:      name,
			container: obj.container,
			object:    obj.object.Name,
			info:      objectInfo(base(name), obj.object),
		}, nil
	}
	if len(files) == 0 && len(dirs) == 0 {
		return nil, notExist(name)
	}
	return &dirFile{src: v, name: name}, nil
}

// Resolve returns the storage container and object name backing the virtual
// path, reporting whether the path names an indexed object. It is used by
// frontends that redirect to the object store rather than stream the bytes.
func (v *View) Resolve(name string) (container, object string, ok bool) {
	if !fs.ValidPath(name) {
		return "", "", false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()
	obj, ok := v.lookupLocked(name)
	if !ok {
		return "", "", false
	}
	return obj.container, obj.object.Name, true
}

// lookupLocked resolves a virtual path to its canonical object. The virtual
// path is tried directly first (so the images layout keeps its container
// prefix), then through the layout's canonical translation.
func (v *View) lookupLocked(name string) (canonicalObject, bool) {
	if obj, ok := v.objects[name]; ok {
		return obj, true
	}
	if canonical, found := v.layout.canonical(name); found {
		obj, ok := v.byObject[canonical]
		return obj, ok
	}
	return canonicalObject{}, false
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
	objects := make(map[string]canonicalObject)
	byObject := make(map[string]canonicalObject)
	var parsed []images.Object
	for _, container := range v.idx.Containers() {
		if legacyContainers[container] {
			continue
		}
		for _, obj := range v.idx.Objects(container) {
			cobj := canonicalObject{container: container, object: obj}
			byObject[obj.Name] = cobj
			for _, name := range v.layout.virtual(container, obj.Name) {
				if name == "" || !fs.ValidPath(name) {
					continue
				}
				objects[name] = cobj
			}
			if o, ok := images.Parse(container, obj.Name); ok {
				parsed = append(parsed, o)
			}
		}
	}
	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
	}
	slices.Sort(names)
	v.objects = objects
	v.byObject = byObject
	v.names = names
	v.catalog = images.NewCatalog(parsed)
	v.version = version
	v.loaded = true
}

// latestSegment is the synthetic path segment naming the newest release of a
// product in a series. It exists only in the HTTP virtual namespace: it is
// never added to the fs.FS, so it does not appear in listings and rsync never
// sees it.
const latestSegment = "latest"

// Latest resolves a virtual path whose release segment is the synthetic
// "latest" segment to the concrete virtual path of the series' newest release
// of the named product. It is used by the HTTP listener to redirect. Only the
// canonical images layout exposes an explicit release and product, so only it
// answers; the legacy layouts report false.
func (v *View) Latest(name string) (string, bool) {
	if v.layout != LayoutImages || !fs.ValidPath(name) {
		return "", false
	}
	parts := strings.Split(name, "/")
	// {container}/{latest}/{product}/{path...}
	if len(parts) < 3 || parts[1] != latestSegment {
		return "", false
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()

	series, ok := v.catalog.ResolveContainer(parts[0])
	if !ok {
		return "", false
	}
	release, ok := v.catalog.Latest(series, parts[2])
	if !ok {
		return "", false
	}
	parts[1] = release.String()
	return strings.Join(parts, "/"), true
}

type virtualObject struct {
	name string
	obj  storage.Object
}

func (v *View) childrenLocked(name string) (files []virtualObject, dirs []string) {
	prefix := strings.Trim(name, "/")
	if prefix == "." {
		prefix = ""
	}
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
		files = append(files, virtualObject{name: n, obj: v.objects[n].object})
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
