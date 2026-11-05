package vfs

import (
	"context"
	"io"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

func TestLayoutVirtual(t *testing.T) {
	cases := []struct {
		layout    Layout
		canonical string
		want      []string
	}{
		{LayoutImages, "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso", []string{"noble/24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"}},
		{LayoutReleases, "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso", []string{"24.04/ubuntu-24.04.3-desktop-amd64.iso", "noble/ubuntu-24.04.3-desktop-amd64.iso"}},
		{LayoutReleases, "24.04/netboot/amd64/linux", []string{"24.04/netboot/amd64/linux"}},
		{LayoutReleases, "24.04/kubuntu/a.iso", nil},
		{LayoutCDImage, "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso", []string{"ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso", "ubuntu/releases/noble/release/ubuntu-24.04.3-desktop-amd64.iso"}},
		{LayoutCDImage, "24.04.3/ubuntu-base/ubuntu-base-24.04.3-base-amd64.tar.gz", []string{"ubuntu-base/releases/24.04/release/ubuntu-base-24.04.3-base-amd64.tar.gz", "ubuntu-base/releases/noble/release/ubuntu-base-24.04.3-base-amd64.tar.gz"}},
		{LayoutCDImage, "24.04/ubuntu-server/noble-live-server-amd64.iso", []string{"ubuntu-server/noble/daily-live/current/noble-live-server-amd64.iso"}},
		{LayoutCDImage, "24.04/ubuntu-server/noble-preinstalled-server-arm64.img.xz", []string{"ubuntu-server/noble/daily-preinstalled/current/noble-preinstalled-server-arm64.img.xz"}},
		{LayoutCDImage, "24.04/ubuntu-wsl/noble-wsl-amd64.wsl", []string{"ubuntu-wsl/noble/daily-live/current/noble-wsl-amd64.wsl"}},
		{LayoutCDImage, "24.04/ubuntu-core/ubuntu-core-24-amd64.img.xz", []string{"ubuntu-core/24/stable/current/ubuntu-core-24-amd64.img.xz"}},
		{LayoutCDImage, "24.04/netboot/amd64/linux", []string{"netboot/noble/amd64/linux"}},
	}
	for _, tc := range cases {
		got := tc.layout.virtual("noble", tc.canonical)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s virtual(%q) = %v; want %v", tc.layout, tc.canonical, got, tc.want)
		}
	}
}

// TestLayoutAliases checks the legacy paths that are not materialised by the
// projection but name the same object, such as dated cdimage build dirs.
func TestLayoutAliases(t *testing.T) {
	cases := []struct {
		layout  Layout
		virtual string
		want    []string
	}{
		{LayoutCDImage, "ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.img.xz", []string{"ubuntu-core/24/stable/current/ubuntu-core-24-amd64.img.xz"}},
		{LayoutCDImage, "ubuntu-server/noble/daily-live/20260908/noble-live-server-amd64.iso", []string{"ubuntu-server/noble/daily-live/current/noble-live-server-amd64.iso"}},
		{LayoutCDImage, "ubuntu-server/noble/daily-live/current/noble-live-server-amd64.iso", nil},
		{LayoutReleases, "24.04/ubuntu-24.04.3-desktop-amd64.iso", nil},
	}
	for _, tc := range cases {
		got := tc.layout.aliases(tc.virtual)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s aliases(%q) = %v; want %v", tc.layout, tc.virtual, got, tc.want)
		}
	}
}

func newLayoutView(t *testing.T, layout Layout) *View {
	t.Helper()
	// The canonical tree is spread across one container per series: the
	// container is the series codename and each object is named
	// {release}/{product}/{path}.
	content := map[string]map[string]string{
		"noble": {
			"24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso":           "desktop",
			"24.04.5/ubuntu/ubuntu-24.04.5-desktop-amd64.iso":           "noble-desktop",
			"24.04.3/ubuntu/SHA256SUMS":                                 "sums",
			"24.04/ubuntu-server/noble-live-server-amd64.iso":           "server",
			"24.04/ubuntu-core/ubuntu-core-24-amd64.img.xz":             "core",
			"24.04/netboot/amd64/linux":                                 "kernel",
			"24.04.3/ubuntu-base/ubuntu-base-24.04.3-base-amd64.tar.gz": "base",
			"24.04/kubuntu/kubuntu-24.04.5-desktop-amd64.iso":           "kubuntu",
		},
		"resolute": {
			"26.04.1/ubuntu/ubuntu-26.04.1-desktop-amd64.iso": "resolute-desktop",
		},
	}
	objects := map[string][]storage.Object{}
	flat := map[string]string{}
	for container, files := range content {
		for name, body := range files {
			objects[container] = append(objects[container], storage.Object{Name: name, Size: int64(len(body))})
			flat[container+"/"+name] = body
		}
	}
	st := fakeStorage{objects: objects, content: flat}
	idx := index.New()
	if err := idx.Refresh(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return NewLayout(context.Background(), idx, st, layout)
}

func TestViewResolve(t *testing.T) {
	cases := []struct {
		layout    Layout
		virtual   string
		container string
		object    string
	}{
		{LayoutImages, "noble/24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso", "noble", "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"},
		{LayoutReleases, "24.04/ubuntu-24.04.3-desktop-amd64.iso", "noble", "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"},
		{LayoutCDImage, "ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso", "noble", "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"},
	}
	for _, tc := range cases {
		v := newLayoutView(t, tc.layout)
		container, object, ok := v.Resolve(tc.virtual)
		if !ok || container != tc.container || object != tc.object {
			t.Errorf("%s Resolve(%q) = %q, %q, %v; want %q, %q, true", tc.layout, tc.virtual, container, object, ok, tc.container, tc.object)
		}
	}
}

func TestViewCDImageTree(t *testing.T) {
	v := newLayoutView(t, LayoutCDImage)
	entries, err := fs.ReadDir(v, "ubuntu")
	if err != nil {
		t.Fatalf("ReadDir ubuntu: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "releases" {
		t.Fatalf("ubuntu entries = %v", entries)
	}
	entries, err = fs.ReadDir(v, "ubuntu/releases/24.04/release")
	if err != nil {
		t.Fatalf("ReadDir release: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, want := range []string{"SHA256SUMS", "ubuntu-24.04.3-desktop-amd64.iso", "ubuntu-24.04.5-desktop-amd64.iso"} {
		if !slices.Contains(names, want) {
			t.Errorf("release entries = %v, missing %q", names, want)
		}
	}
	if got := readFile(t, v, "ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso"); got != "desktop" {
		t.Errorf("content = %q", got)
	}
	if got := readFile(t, v, "ubuntu-server/noble/daily-live/current/noble-live-server-amd64.iso"); got != "server" {
		t.Errorf("daily content = %q", got)
	}
	if got := readFile(t, v, "ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.img.xz"); got != "core" {
		t.Errorf("dated core content = %q", got)
	}
	if got := readFile(t, v, "netboot/noble/amd64/linux"); got != "kernel" {
		t.Errorf("netboot content = %q", got)
	}
}

func TestViewReleasesTree(t *testing.T) {
	v := newLayoutView(t, LayoutReleases)
	if got := readFile(t, v, "24.04/ubuntu-24.04.3-desktop-amd64.iso"); got != "desktop" {
		t.Errorf("content = %q", got)
	}
	if got := readFile(t, v, "noble/ubuntu-24.04.5-desktop-amd64.iso"); got != "noble-desktop" {
		t.Errorf("noble content = %q", got)
	}
	if got := readFile(t, v, "24.04/netboot/amd64/linux"); got != "kernel" {
		t.Errorf("netboot content = %q", got)
	}
	entries, err := fs.ReadDir(v, "24.04")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, want := range []string{"netboot", "ubuntu-24.04.3-desktop-amd64.iso"} {
		if !slices.Contains(names, want) {
			t.Errorf("24.04 entries = %v, missing %q", names, want)
		}
	}
	for _, unwanted := range []string{"kubuntu-24.04.5-desktop-amd64.iso"} {
		if slices.Contains(names, unwanted) {
			t.Errorf("24.04 entries = %v, unexpectedly contains %q", names, unwanted)
		}
	}
}

func TestViewImagesTree(t *testing.T) {
	v := newLayoutView(t, LayoutImages)
	// The images layout keeps the canonical tree but prefixes each path with
	// the object's (per-series) container.
	if got := readFile(t, v, "noble/24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"); got != "desktop" {
		t.Errorf("content = %q", got)
	}
	if got := readFile(t, v, "noble/24.04/kubuntu/kubuntu-24.04.5-desktop-amd64.iso"); got != "kubuntu" {
		t.Errorf("kubuntu content = %q", got)
	}
	if got := readFile(t, v, "resolute/26.04.1/ubuntu/ubuntu-26.04.1-desktop-amd64.iso"); got != "resolute-desktop" {
		t.Errorf("resolute content = %q", got)
	}
}

// TestViewLatest checks the HTTP-only synthetic "latest" segment, resolved
// from the discovered catalog rather than being part of the tree.
func TestViewLatest(t *testing.T) {
	st := fakeStorage{
		objects: map[string][]storage.Object{
			"noble": {
				{Name: "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"},
				{Name: "24.04.5/ubuntu/ubuntu-24.04.5-desktop-amd64.iso"},
				{Name: "24.04.5/ubuntu/SHA256SUMS"},
				{Name: "24.04/ubuntu-server/noble-live-server-amd64.iso"},
			},
			"resolute": {
				{Name: "26.04.1/ubuntu/ubuntu-26.04.1-desktop-amd64.iso"},
			},
		},
		content: map[string]string{"noble/24.04.5/ubuntu/SHA256SUMS": "sums"},
	}
	idx := index.New()
	if err := idx.Refresh(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	v := NewLayout(context.Background(), idx, st, LayoutImages)

	cases := []struct {
		virtual string
		want    string
		ok      bool
	}{
		{"noble/latest/ubuntu", "noble/24.04.5/ubuntu", true},
		{"noble/latest/ubuntu/ubuntu-24.04.5-desktop-amd64.iso", "noble/24.04.5/ubuntu/ubuntu-24.04.5-desktop-amd64.iso", true},
		{"noble/latest/ubuntu-server", "noble/24.04/ubuntu-server", true},
		{"resolute/latest/ubuntu", "resolute/26.04.1/ubuntu", true},
		{"noble/latest/nope", "", false},
		{"noble/latest", "", false},
		{"nope/latest/ubuntu", "", false},
	}
	for _, tc := range cases {
		got, ok := v.Latest(tc.virtual)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Latest(%q) = %q, %v; want %q, %v", tc.virtual, got, ok, tc.want, tc.ok)
		}
	}

	// The synthetic segment is not part of the fs.FS, so rsync never sees it,
	// and it does not leak into listings.
	if _, err := fs.Stat(v, "noble/latest/ubuntu"); err == nil {
		t.Error("latest segment is visible in the fs.FS")
	}
	entries, err := fs.ReadDir(v, "noble")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "latest" {
			t.Errorf("latest leaked into the noble listing: %v", entries)
		}
	}
}

func TestViewRootListing(t *testing.T) {
	cases := []struct {
		layout Layout
		want   []string
	}{
		{LayoutImages, []string{"noble", "resolute"}},
		{LayoutReleases, []string{"24.04", "26.04", "noble", "resolute"}},
		{LayoutCDImage, []string{"kubuntu", "netboot", "ubuntu", "ubuntu-base", "ubuntu-core", "ubuntu-server"}},
	}
	for _, tc := range cases {
		v := newLayoutView(t, tc.layout)
		entries, err := fs.ReadDir(v, ".")
		if err != nil {
			t.Fatalf("%s ReadDir root: %v", tc.layout, err)
		}
		got := make([]string, 0, len(entries))
		for _, e := range entries {
			got = append(got, e.Name())
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s root entries = %v, want %v", tc.layout, got, tc.want)
		}
	}
}

// TestViewAggregatesContainers checks that the canonical tree is built from
// object names across all containers (one per series, as in the object store)
// and that each object keeps the container it came from. Legacy layout
// containers are ignored.
func TestViewAggregatesContainers(t *testing.T) {
	st := fakeStorage{
		objects: map[string][]storage.Object{
			"focal":    {{Name: "20.04/ubuntu/focal.iso", Size: 5}},
			"noble":    {{Name: "24.04/ubuntu/noble.iso", Size: 5}},
			"releases": {{Name: "HEADER.html", Size: 9}},
		},
		content: map[string]string{
			"focal/20.04/ubuntu/focal.iso": "focal",
			"noble/24.04/ubuntu/noble.iso": "noble",
		},
	}
	idx := index.New()
	if err := idx.Refresh(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	v := NewLayout(context.Background(), idx, st, LayoutReleases)

	entries, err := fs.ReadDir(v, ".")
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if want := "20.04,24.04,focal,noble"; strings.Join(got, ",") != want {
		t.Fatalf("root entries = %v, want %v", got, want)
	}

	// Content must be streamed from the object's own container.
	if c := readFile(t, v, "24.04/noble.iso"); c != "noble" {
		t.Errorf("noble content = %q", c)
	}
	if c := readFile(t, v, "20.04/focal.iso"); c != "focal" {
		t.Errorf("focal content = %q", c)
	}

	// The legacy "releases" container must not leak into the canonical tree.
	if _, err := v.Open("HEADER.html"); err == nil {
		t.Errorf("legacy releases object unexpectedly visible")
	}

	// The images layout keeps the container as the top-level path segment.
	vi := NewLayout(context.Background(), idx, st, LayoutImages)
	entries, err = fs.ReadDir(vi, ".")
	if err != nil {
		t.Fatalf("images ReadDir root: %v", err)
	}
	got = got[:0]
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if want := "focal,noble"; strings.Join(got, ",") != want {
		t.Fatalf("images root entries = %v, want %v", got, want)
	}
	if c := readFile(t, vi, "noble/24.04/ubuntu/noble.iso"); c != "noble" {
		t.Errorf("images noble content = %q", c)
	}
	if c := readFile(t, vi, "focal/20.04/ubuntu/focal.iso"); c != "focal" {
		t.Errorf("images focal content = %q", c)
	}
}

func readFile(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()
	f, err := fsys.Open(name)
	if err != nil {
		t.Fatalf("Open %q: %v", name, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll %q: %v", name, err)
	}
	return string(b)
}
