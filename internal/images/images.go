// Package images understands the canonical images tree kept in the object
// store. The tree is spread across one container per series (named after the
// series codename) and names each object {release}/{product}/{path}, where
// release is a versioned release such as 24.04.3 or a named milestone such as
// "24.04-beta". Artifact file names are made of underscore-separated fields
// ({project}[_{subproject}]_{version}_{arch}_{YYYYMMDD}{ext}), so the project,
// optional subproject, architecture and build date are part of the parsed
// model. This package parses those names into a semantic model so callers can
// reason about series, point releases, products, artifacts and architectures
// instead of raw strings.
//
// Nothing here is configured or hard-coded per series: the model only encodes
// the grammar of Ubuntu versions and object names, and every fact about a
// particular series, product or release is read from the parsed tree.
package images

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a dot-separated sequence of decimal components, as used for
// Ubuntu release versions: 24.04, 24.04.3 or 24.04.5.1. It is comparable and
// orders component by component.
type Version []int

// ParseVersion parses a dotted decimal version. It reports false unless s is a
// non-empty sequence of decimal components with no empty part.
func ParseVersion(s string) (Version, bool) {
	if s == "" {
		return nil, false
	}
	parts := strings.Split(s, ".")
	v := make(Version, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		v = append(v, n)
	}
	return v, true
}

// String renders the version back to its canonical form. The series month
// (the second component) is zero-padded to two digits, matching Ubuntu's
// year.month convention.
func (v Version) String() string {
	var b strings.Builder
	for i, n := range v {
		if i > 0 {
			b.WriteByte('.')
		}
		if i == 1 && n < 10 {
			b.WriteByte('0')
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}

// Compare orders v against w component by component, treating a missing
// component as zero. So 24.04 == 24.04.0 < 24.04.3 < 24.04.5.1.
func (v Version) Compare(w Version) int {
	for i := 0; i < max(len(v), len(w)); i++ {
		a, b := 0, 0
		if i < len(v) {
			a = v[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Series returns the year.month pair identifying the release series, and
// reports false for a version with fewer than two components.
func (v Version) Series() (Series, bool) {
	if len(v) < 2 {
		return Series{}, false
	}
	return Series{Major: v[0], Minor: v[1]}, true
}

// Series identifies an Ubuntu release series by its year and month, for
// example 24.04. All point releases of a series (24.04, 24.04.3, 24.04.5.1)
// share the same Series.
type Series struct {
	Major int // year, e.g. 24
	Minor int // month, e.g. 4
}

func (s Series) String() string {
	return fmt.Sprintf("%d.%02d", s.Major, s.Minor)
}

// Release is the first path segment of a canonical object name. It is either
// a versioned release (24.04, 24.04.3) or a named milestone such as "beta",
// which carries no version of its own.
type Release struct {
	Version   Version // set for a versioned release, nil otherwise
	Milestone string  // set for a named milestone, empty otherwise
}

// ParseRelease parses a release segment: a dotted version, or any other
// single path element treated as a milestone name.
func ParseRelease(s string) (Release, bool) {
	if s == "" || strings.Contains(s, "/") {
		return Release{}, false
	}
	if v, ok := ParseVersion(s); ok {
		return Release{Version: v}, true
	}
	return Release{Milestone: s}, true
}

// IsVersion reports whether the release is versioned rather than a milestone.
func (r Release) IsVersion() bool { return r.Version != nil }

func (r Release) String() string {
	if r.IsVersion() {
		return r.Version.String()
	}
	return r.Milestone
}

// Series returns the release's series, reporting false for a milestone or a
// version without a year.month pair.
func (r Release) Series() (Series, bool) {
	if !r.IsVersion() {
		return Series{}, false
	}
	return r.Version.Series()
}

// Artifact is the parsed name of an artifact file. Canonical artifact names
// are underscore-separated fields:
//
//	{project}[_{subproject}]_{version}_{arch}_{YYYYMMDD}{ext}
//
// Project names may contain hyphens. The optional subproject is empty when the
// name has no second project field. Version keeps the spelling from the file
// (24.04.3, 24 or 24.04-beta), and Extension keeps every suffix after the
// build date together with its leading dots (.iso, .tar.gz, .iso.sha256.gpg).
type Artifact struct {
	Project      string
	Subproject   string // optional
	Version      string
	Architecture string
	Date         string
	Extension    string
}

// Object is a canonical object name parsed into its semantic parts. The name
// is {release}/{product}/{path} and the container is the series codename. The
// embedded Artifact fields are zero when the path is not an artifact name, such
// as a shared metadata file or a netboot directory.
type Object struct {
	Container string
	Release   Release
	Product   string
	Artifact
	Path string // remainder after {release}/{product}/, possibly empty
}

// Parse splits a canonical object name, optionally qualified by its container,
// into its parts. It reports false for names that do not carry at least a
// release and a product.
func Parse(container, name string) (Object, bool) {
	release, rest, ok := strings.Cut(name, "/")
	if !ok {
		return Object{}, false
	}
	product, path, _ := strings.Cut(rest, "/")
	rel, ok := ParseRelease(release)
	if !ok || product == "" {
		return Object{}, false
	}
	artifact, _ := ParseArtifact(path)
	return Object{Container: container, Release: rel, Product: product, Artifact: artifact, Path: path}, true
}

// ParseArtifact parses the project, optional subproject, version, architecture,
// build date and extension from an artifact path. It reports false for names
// that do not follow the canonical shape, such as shared metadata files
// (SHA256SUMS) or a bare directory path.
func ParseArtifact(path string) (Artifact, bool) {
	name := path
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	fields := strings.Split(name, "_")
	if len(fields) < 4 {
		return Artifact{}, false
	}

	dateExt := fields[len(fields)-1]
	date, ext, _ := strings.Cut(dateExt, ".")
	if len(date) != 8 || !allDigits(date) {
		return Artifact{}, false
	}
	if ext != "" {
		ext = "." + ext
	}

	arch := fields[len(fields)-2]
	if !validArchitecture(arch) {
		return Artifact{}, false
	}
	version := fields[len(fields)-3]
	if !validArtifactVersion(version) {
		return Artifact{}, false
	}

	prefix := fields[:len(fields)-3]
	if len(prefix) < 1 || len(prefix) > 2 || prefix[0] == "" {
		return Artifact{}, false
	}
	subproject := ""
	if len(prefix) == 2 {
		subproject = prefix[1]
		if subproject == "" {
			return Artifact{}, false
		}
	}

	return Artifact{
		Project:      prefix[0],
		Subproject:   subproject,
		Version:      version,
		Architecture: arch,
		Date:         date,
		Extension:    ext,
	}, true
}

// ParseArchitecture parses the architecture from an artifact path, reporting
// false for names that are not artifacts or do not carry a known architecture.
func ParseArchitecture(path string) (string, bool) {
	artifact, ok := ParseArtifact(path)
	if !ok {
		return "", false
	}
	return artifact.Architecture, true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// validArtifactVersion accepts the version field as written in an artifact
// name: a dotted numeric version, optionally followed by a milestone suffix
// such as 24.04-beta.
func validArtifactVersion(s string) bool {
	base, milestone, hasMilestone := strings.Cut(s, "-")
	if _, ok := ParseVersion(base); !ok {
		return false
	}
	if !hasMilestone {
		return true
	}
	if milestone == "" {
		return false
	}
	for i := 0; i < len(milestone); i++ {
		c := milestone[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// validArchitecture reports whether s is one of the architecture identifiers
// Ubuntu builds and publishes images for. i386 is included because older
// releases still carry 32-bit images, even though it is now only a partial
// port.
func validArchitecture(s string) bool {
	switch s {
	case "amd64", "arm64", "armhf", "i386", "ppc64el", "riscv64", "s390x":
		return true
	}
	return false
}

// Series returns the object's series, reporting false for an object whose
// release is a milestone.
func (o Object) Series() (Series, bool) { return o.Release.Series() }
