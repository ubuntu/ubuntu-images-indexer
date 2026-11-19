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

// Layouts lists every known layout. It is the single registry the HTTP host
// table, the rsync modules and the configuration validation all refer to.
func Layouts() []Layout {
	return []Layout{LayoutCDImage, LayoutReleases, LayoutImages}
}

// ParseLayout returns the layout with the given module name.
func ParseLayout(name string) (Layout, bool) {
	for _, l := range Layouts() {
		if l == Layout(name) {
			return l, true
		}
	}
	return "", false
}

// virtual translates a canonical object name in container to the paths this
// layout exposes. The projection is derived from the parsed object: the
// release series names the legacy directory, both by version and by the
// container's codename wherever the legacy tree keeps both, and artifact names
// are rebuilt in the historical hyphen-separated form from their parsed
// project, subproject, version, architecture and extension.
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
				return d + "/" + releaseName(obj)
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
				return []string{fmt.Sprintf("ubuntu-core/%d/stable/current/%s", series.Major, coreName(obj))}
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
			return []string{obj.Product + "/" + d + "/" + channel + "/current/" + dailyName(obj, d)}
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
				return obj.Product + "/releases/" + d + "/release/" + releaseName(obj)
			})
		}
	}
	return nil
}

// legacyName joins the non-empty parts into the historical hyphen-separated
// cdimage filename and appends the artifact extension. It carries the canonical
// path unchanged when the object is not an artifact (shared metadata, netboot
// trees and so on).
func legacyName(obj images.Object, parts ...string) string {
	if obj.Project == "" {
		return obj.Path
	}
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('-')
		}
		b.WriteString(part)
	}
	b.WriteString(obj.Extension)
	return b.String()
}

// repeatedSubproject names the projects whose historical release filenames
// repeat their project suffix as the subproject, such as
// ubuntu-base-24.04.3-base-amd64.tar.gz. The canonical name does not repeat it.
var repeatedSubproject = map[string]string{
	"ubuntu-base":     "base",
	"ubuntu-mini-iso": "mini-iso",
}

// releaseName reconstructs <project>-<version>[-<subproject>]-<arch><ext>, the
// filename used by the release trees.
func releaseName(obj images.Object) string {
	subproject := obj.Subproject
	if subproject == "" {
		subproject = repeatedSubproject[obj.Product]
	}
	return legacyName(obj, obj.Project, obj.Version, subproject, obj.Architecture)
}

// dailyName reconstructs the daily cdimage filenames, which are keyed by the
// series codename and put the project role last:
// <codename>[-<subproject>]-<role>-<arch><ext>.
func dailyName(obj images.Object, codename string) string {
	role := obj.Project
	if suffix, ok := strings.CutPrefix(obj.Project, "ubuntu-"); ok {
		role = suffix
	}
	return legacyName(obj, codename, obj.Subproject, role, obj.Architecture)
}

// coreName reconstructs <project>[-<subproject>]-<version>-<arch><ext>, such
// as ubuntu-core-24-amd64.img.xz.
func coreName(obj images.Object) string {
	return legacyName(obj, obj.Project, obj.Subproject, obj.Version, obj.Architecture)
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

// View is a read-only fs.FS exposing the canonical images tree through a
// legacy layout. The canonical tree is spread across the object store's
// containers: the container is the series codename and each object's name is
// {release}/{product}/{path}.
//
// The projection is materialised eagerly, once per index snapshot: the first
// access after an index refresh rebuilds the virtual path table from the
// shared Catalog's index.
type View struct {
	fsView
	idx    *index.Index
	cat    *Catalog // shared semantic model of the canonical tree
	layout Layout

	mu      sync.RWMutex
	version uint64
	loaded  bool
	objects map[string]canonicalObject // virtual path -> object
	links   map[string]string          // virtual path -> symlink target
	names   []string
}

// View returns a view of the canonical images tree through layout l, reading
// structure from the catalog's index and contents from st. Streams are opened
// with ctx so that shutdown cancels in-flight transfers.
func (c *Catalog) View(ctx context.Context, st storage.Storage, l Layout) *View {
	v := &View{
		fsView: fsView{ctx: ctx, st: st},
		idx:    c.idx,
		cat:    c,
		layout: l,
	}
	v.m = v
	return v
}

// resolve implements pathMapper.
func (v *View) resolve(name string) (mapping, bool) {
	if !fs.ValidPath(name) {
		return mapping{}, false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()
	path, ok := v.followLocked(name)
	if !ok {
		return mapping{}, false
	}
	if obj, ok := v.lookupLocked(path); ok {
		return mapping{obj: obj, path: path}, true
	}
	files, dirs := v.childrenLocked(path)
	if len(files) > 0 || len(dirs) > 0 || path == "." {
		return mapping{dir: true, path: path}, true
	}
	return mapping{}, false
}

// entries implements pathMapper.
func (v *View) entries(name string) []fs.DirEntry {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()
	if path, ok := v.followLocked(name); ok {
		name = path
	}
	return v.entriesLocked(name)
}

// followLocked resolves symlinks in name, returning the virtual path they lead
// to. Links may point at directories or objects, and may chain.
func (v *View) followLocked(name string) (string, bool) {
	if name == "." {
		return ".", true
	}
	pending := strings.Split(name, "/")
	var resolved []string
	hops := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) == 0 {
				return "", false
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		resolved = append(resolved, part)
		target, ok := v.links[strings.Join(resolved, "/")]
		if !ok {
			continue
		}
		hops++
		if hops > 40 {
			return "", false
		}
		resolved = resolved[:len(resolved)-1]
		pending = append(strings.Split(target, "/"), pending...)
	}
	if len(resolved) == 0 {
		return ".", true
	}
	return strings.Join(resolved, "/"), true
}

// readLink implements linkMapper.
func (v *View) readLink(name string) (string, bool) {
	if !fs.ValidPath(name) {
		return "", false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()
	return v.readLinkLocked(name)
}

// readLinkLocked returns the target of the symlink at name. Symlinks in the
// parent components are followed, but the final component is not.
func (v *View) readLinkLocked(name string) (string, bool) {
	if name == "." {
		return "", false
	}
	dir, file := "", name
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		dir, file = name[:i], name[i+1:]
	}
	parent := "."
	if dir != "" {
		resolved, ok := v.followLocked(dir)
		if !ok {
			return "", false
		}
		parent = resolved
	}
	path := file
	if parent != "." {
		path = parent + "/" + file
	}
	target, ok := v.links[path]
	return target, ok
}

// lstat implements linkMapper.
func (v *View) lstat(name string) (fs.FileInfo, bool) {
	if !fs.ValidPath(name) {
		return nil, false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()
	target, ok := v.readLinkLocked(name)
	if !ok {
		return nil, false
	}
	return symlinkInfo(base(name), target), true
}

// Resolve returns the storage container and object name backing the virtual
// path, reporting whether the path names an indexed object. It is used by
// frontends that redirect to the object store rather than stream the bytes.
func (v *View) Resolve(name string) (container, object string, ok bool) {
	m, ok := v.resolve(name)
	if !ok || m.dir {
		return "", "", false
	}
	return m.obj.container, m.obj.object.Name, true
}

// Resolution describes a resolved path in a view: the indexed object it names,
// the entries of the directory it names, or a symlink.
type Resolution struct {
	Container string         // set when the path names an indexed object
	Object    storage.Object // ditto
	Entries   []fs.DirEntry  // set when the path names a directory

	Link   string // literal target when the path names a symlink
	Target string // resolved virtual path the symlink points at
}

// IsObject reports whether the path names an indexed object rather than a
// directory or symlink.
func (r Resolution) IsObject() bool { return r.Entries == nil && r.Link == "" }

// IsLink reports whether the path itself names a symlink.
func (r Resolution) IsLink() bool { return r.Link != "" }

// ResolvePath resolves a virtual path to the canonical object backing it, the
// entries of the directory it names, or the symlink it is, reporting whether
// the path exists at all. It is the single lookup the HTTP listener needs:
// object requests redirect to the store, directories render their entries, and
// symlinks redirect to their target.
func (v *View) ResolvePath(name string) (Resolution, bool) {
	if !fs.ValidPath(name) {
		return Resolution{}, false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ensureLocked()
	if name == "." {
		return Resolution{Entries: v.entriesLocked(name)}, true
	}
	if target, ok := v.readLinkLocked(name); ok {
		path, ok := v.followLocked(name)
		if !ok {
			return Resolution{}, false
		}
		return Resolution{Link: target, Target: path}, true
	}
	path, ok := v.followLocked(name)
	if !ok {
		return Resolution{}, false
	}
	if obj, ok := v.lookupLocked(path); ok {
		return Resolution{Container: obj.container, Object: obj.object}, true
	}
	files, dirs := v.childrenLocked(path)
	if len(files) == 0 && len(dirs) == 0 && path != "." {
		return Resolution{}, false
	}
	return Resolution{Entries: v.entriesLocked(path)}, true
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

func (v *View) ensureLocked() {
	version := v.idx.Version()
	if v.loaded && v.version == version {
		return
	}
	objects := make(map[string]canonicalObject)
	for _, container := range v.idx.Containers() {
		if legacyContainers[container] {
			continue
		}
		for _, obj := range v.idx.Objects(container) {
			cobj := canonicalObject{container: container, object: obj}
			if o, ok := images.Parse(container, obj.Name); ok {
				cobj.release = o.Release
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
	if v.layout == LayoutCDImage {
		addChecksumAliases(objects)
	}
	links := make(map[string]string)
	if v.layout == LayoutImages {
		links = v.latestLinksLocked(objects)
	}
	names := make([]string, 0, len(objects)+len(links))
	for name := range objects {
		names = append(names, name)
	}
	for name := range links {
		names = append(names, name)
	}
	slices.Sort(names)
	v.objects = objects
	v.links = links
	v.names = names
	v.version = version
	v.loaded = true
}

// latestLinksLocked returns the "latest" symlinks of the canonical layout. Each
// series container gets a link to the newest release version in that series, so
// clients can keep using an unchanging path while the link follows every new
// point release.
func (v *View) latestLinksLocked(objects map[string]canonicalObject) map[string]string {
	links := make(map[string]string)
	catalog := v.cat.Load()
	for _, container := range v.idx.Containers() {
		if legacyContainers[container] {
			continue
		}
		series, ok := catalog.ResolveContainer(container)
		if !ok {
			continue
		}
		latest, ok := catalog.LatestRelease(series)
		if !ok {
			continue
		}
		name := container + "/" + latestSegment
		if _, ok := objects[name]; ok {
			continue
		}
		files, dirs := v.idx.Children(container, latest.String())
		if len(files) == 0 && len(dirs) == 0 {
			continue
		}
		links[name] = latest.String()
	}
	return links
}

// addChecksumAliases materialises the folder-level checksum files the old
// cdimage tree kept next to every set of images: SHA256SUMS and SHA256SUMS.gpg.
// The canonical tree keeps a checksum per artifact instead, so each alias takes
// the first artifact checksum, in name order, whose image is served in the same
// directory. A real SHA256SUMS object in the canonical tree is left untouched.
func addChecksumAliases(objects map[string]canonicalObject) {
	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
	}
	slices.Sort(names)

	sums := make(map[string]canonicalObject)
	signatures := make(map[string]canonicalObject)
	for _, name := range names {
		i := strings.LastIndexByte(name, '/')
		if i < 0 {
			continue
		}
		dir, file := name[:i+1], name[i+1:]
		image, ok := checksumImage(file)
		if !ok {
			continue
		}
		if _, ok := objects[dir+image]; !ok {
			continue
		}
		switch {
		case strings.HasSuffix(file, ".sha256.gpg"):
			if _, ok := signatures[dir]; !ok {
				signatures[dir] = objects[name]
			}
		case strings.HasSuffix(file, ".sha256"):
			if _, ok := sums[dir]; !ok {
				sums[dir] = objects[name]
			}
		}
	}
	for dir, obj := range sums {
		if _, ok := objects[dir+"SHA256SUMS"]; !ok {
			objects[dir+"SHA256SUMS"] = obj
		}
	}
	for dir, obj := range signatures {
		if _, ok := objects[dir+"SHA256SUMS.gpg"]; !ok {
			objects[dir+"SHA256SUMS.gpg"] = obj
		}
	}
}

// checksumImage returns the image name a per-artifact checksum file belongs to.
func checksumImage(file string) (string, bool) {
	if image, ok := strings.CutSuffix(file, ".sha256.gpg"); ok {
		return image, true
	}
	if image, ok := strings.CutSuffix(file, ".sha256"); ok {
		return image, true
	}
	return "", false
}

// latestSegment names the canonical layout's latest symlink and the synthetic
// HTTP release segment that resolves it per product.
const latestSegment = "latest"

// Latest resolves a path whose release segment is the synthetic "latest"
// segment to the concrete virtual path of the series' newest release of the
// named product. The canonical layout also materialises a series-level "latest"
// symlink; this per-product lookup is HTTP-only and handles paths that descend
// through it. Only the canonical images layout exposes an explicit release and
// product, so only it answers; the legacy layouts report false.
func (v *View) Latest(name string) (string, bool) {
	if v.layout != LayoutImages || !fs.ValidPath(name) {
		return "", false
	}
	parts := strings.Split(name, "/")
	// {container}/{latest}/{product}/{path...}
	if len(parts) < 3 || parts[1] != latestSegment {
		return "", false
	}

	catalog := v.cat.Load()
	series, ok := catalog.ResolveContainer(parts[0])
	if !ok {
		return "", false
	}
	release, ok := catalog.Latest(series, parts[2])
	if !ok {
		return "", false
	}
	parts[1] = release.String()
	return strings.Join(parts, "/"), true
}

type virtualObject struct {
	name string
	obj  storage.Object
	link string // non-empty when the entry is a symlink
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
		if obj, ok := v.objects[n]; ok {
			files = append(files, virtualObject{name: n, obj: obj.object})
		} else if target, ok := v.links[n]; ok {
			files = append(files, virtualObject{name: n, link: target})
		}
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
		if f.link != "" {
			out = append(out, dirEntry{name: base(f.name), info: symlinkInfo(base(f.name), f.link)})
			continue
		}
		out = append(out, dirEntry{name: base(f.name), info: objectInfo(base(f.name), f.obj)})
	}
	return out
}
