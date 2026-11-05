package vfs

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/ubuntu/ubimi/internal/images"
	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

// Layout identifies a legacy tree layout exposed over rsync. The object store
// itself keeps the new canonical layout: one container per series, named after
// the series codename, holding {release}/{product}/{path} object names.
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

// virtual translates a canonical object name in container to the paths this
// layout exposes. The projection is derived from the parsed object: the
// release series names the legacy directory, both by version and by the
// container's codename wherever the legacy tree keeps both, and the object
// path is carried through unchanged.
//
// The images layout instead keeps the canonical tree, prefixed with the
// object's per-series container so that the containers stay visible.
func (l Layout) virtual(container, name string) []string {
	if name == "" || name == "." || !fs.ValidPath(name) {
		return nil
	}
	if l == LayoutImages {
		return []string{container + "/" + name}
	}
	if !strings.Contains(name, "/") {
		return []string{name} // shared root files such as HEADER.html
	}

	obj, ok := images.Parse(container, name)
	if !ok {
		return nil
	}
	series, versioned := obj.Series()

	switch l {
	case LayoutReleases:
		switch obj.Product {
		case "ubuntu":
			return expand(releaseDirs(series, versioned, container), func(d string) string {
				return d + "/" + obj.Path
			})
		case "netboot":
			// releases.ubuntu.com keys netboot by version.
			if versioned {
				return []string{series.String() + "/netboot/" + obj.Path}
			}
			if container != "" {
				return []string{container + "/netboot/" + obj.Path}
			}
		}

	case LayoutCDImage:
		switch obj.Product {
		case "ubuntu-core":
			if versioned {
				return []string{fmt.Sprintf("ubuntu-core/%d/stable/current/%s", series.Major, obj.Path)}
			}
		case "ubuntu-server", "ubuntu-wsl":
			d := releaseDir(series, versioned, container)
			if d == "" {
				break
			}
			channel := "daily-live"
			if obj.Product == "ubuntu-server" && strings.Contains(obj.Path, "preinstalled") {
				channel = "daily-preinstalled"
			}
			return []string{obj.Product + "/" + d + "/" + channel + "/current/" + obj.Path}
		case "netboot":
			// cdimage.ubuntu.com keys netboot by codename.
			if container != "" {
				return []string{"netboot/" + container + "/" + obj.Path}
			}
			if versioned {
				return []string{"netboot/" + series.String() + "/" + obj.Path}
			}
		default:
			return expand(releaseDirs(series, versioned, container), func(d string) string {
				return obj.Product + "/releases/" + d + "/release/" + obj.Path
			})
		}
	}
	return nil
}

// aliases returns other virtual paths that name the same object but are not
// materialised by virtual, such as the dated build directories the cdimage
// tree keeps alongside its "current" one.
func (l Layout) aliases(name string) []string {
	if l != LayoutCDImage {
		return nil
	}
	parts := strings.Split(name, "/")
	if len(parts) < 5 || parts[3] == "current" {
		return nil
	}
	daily := strings.HasPrefix(parts[2], "daily-")
	stable := parts[0] == "ubuntu-core" && parts[2] == "stable"
	if !daily && !stable {
		return nil
	}
	parts[3] = "current"
	return []string{strings.Join(parts, "/")}
}

// releaseDirs returns the directory names a legacy tree uses for a series: its
// version and, where different, the container's codename.
func releaseDirs(series images.Series, versioned bool, codename string) []string {
	var dirs []string
	if versioned {
		dirs = append(dirs, series.String())
	}
	if codename != "" && (!versioned || codename != series.String()) {
		dirs = append(dirs, codename)
	}
	return dirs
}

// releaseDir returns the single directory a legacy tree uses where only one is
// meaningful: the codename where the tree is keyed by codename, else the
// version.
func releaseDir(series images.Series, versioned bool, codename string) string {
	if codename != "" {
		return codename
	}
	if versioned {
		return series.String()
	}
	return ""
}

// expand maps each directory to a virtual path.
func expand(dirs []string, format func(string) string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, format(d))
	}
	return out
}

// canonicalObject pairs a canonical object with the storage container it
// lives in, so its bytes can be streamed on demand.
type canonicalObject struct {
	container string
	object    storage.Object
	release   images.Release // used to keep the newest release on a path clash
}

// View is a read-only fs.FS exposing the canonical images tree through a
// legacy layout. The canonical tree is spread across the object store's
// containers: the container is the series codename and each object's name is
// {release}/{product}/{path}.
type View struct {
	ctx    context.Context
	idx    *index.Index
	st     storage.Storage
	layout Layout

	mu      sync.RWMutex
	version uint64
	loaded  bool
	objects map[string]canonicalObject // virtual path -> object
	names   []string
	catalog *images.Catalog // series/products/latest discovered from the tree
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

// lookupLocked resolves a virtual path to its canonical object. Most paths are
// materialised directly by the layout projection; a few legacy aliases (dated
// build directories) are normalised to their materialised equivalent first.
func (v *View) lookupLocked(name string) (canonicalObject, bool) {
	if obj, ok := v.objects[name]; ok {
		return obj, true
	}
	for _, alias := range v.layout.aliases(name) {
		if obj, ok := v.objects[alias]; ok {
			return obj, true
		}
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
	var parsed []images.Object
	for _, container := range v.idx.Containers() {
		if legacyContainers[container] {
			continue
		}
		for _, obj := range v.idx.Objects(container) {
			cobj := canonicalObject{container: container, object: obj}
			if o, ok := images.Parse(container, obj.Name); ok {
				cobj.release = o.Release
				parsed = append(parsed, o)
			}
			for _, name := range v.layout.virtual(container, obj.Name) {
				if name == "" || !fs.ValidPath(name) {
					continue
				}
				// A legacy path can be shared by several releases of a
				// series (e.g. per-point SHA256SUMS); keep the newest.
				if prev, ok := objects[name]; ok && prev.release.Version.Compare(cobj.release.Version) >= 0 {
					continue
				}
				objects[name] = cobj
			}
		}
	}
	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
	}
	slices.Sort(names)
	v.objects = objects
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
