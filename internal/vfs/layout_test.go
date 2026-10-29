package vfs

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

func TestLayoutCanonical(t *testing.T) {
	cases := []struct {
		layout  Layout
		virtual string
		want    string
	}{
		{LayoutImages, "24.04/ubuntu/a.iso", "24.04/ubuntu/a.iso"},
		{LayoutReleases, "24.04/ubuntu-24.04.3-desktop-amd64.iso", "24.04/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"},
		{LayoutReleases, "noble/ubuntu-24.04.5-desktop-amd64.iso", "noble/ubuntu/ubuntu-24.04.5-desktop-amd64.iso"},
		{LayoutReleases, "24.04/netboot/amd64/linux", "24.04/netboot/amd64/linux"},
		{LayoutCDImage, "ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso", "24.04/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"},
		{LayoutCDImage, "ubuntu-base/releases/noble/release/ubuntu-base-24.04.3-base-amd64.tar.gz", "noble/ubuntu-base/ubuntu-base-24.04.3-base-amd64.tar.gz"},
		{LayoutCDImage, "ubuntu-server/noble/daily-live/current/noble-live-server-amd64.iso", "noble/ubuntu-server/noble-live-server-amd64.iso"},
		{LayoutCDImage, "ubuntu-server/noble/daily-live/20260908/noble-live-server-amd64.iso", "noble/ubuntu-server/noble-live-server-amd64.iso"},
		{LayoutCDImage, "ubuntu-wsl/noble/daily-live/current/noble-wsl-amd64.wsl", "noble/ubuntu-wsl/noble-wsl-amd64.wsl"},
		{LayoutCDImage, "ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.img.xz", "24.04/ubuntu-core/ubuntu-core-24-amd64.img.xz"},
		{LayoutCDImage, "netboot/focal/index.html", "focal/netboot/index.html"},
		{LayoutCDImage, "HEADER.html", "HEADER.html"},
	}
	for _, tc := range cases {
		got, ok := tc.layout.canonical(tc.virtual)
		if !ok || got != tc.want {
			t.Errorf("%s canonical(%q) = %q, %v; want %q", tc.layout, tc.virtual, got, ok, tc.want)
		}
	}
}

func TestLayoutVirtual(t *testing.T) {
	cases := []struct {
		layout    Layout
		canonical string
		want      []string
	}{
		{LayoutImages, "24.04/ubuntu/a.iso", []string{"24.04/ubuntu/a.iso"}},
		{LayoutReleases, "24.04/ubuntu/a.iso", []string{"24.04/a.iso"}},
		{LayoutReleases, "noble/ubuntu/a.iso", []string{"noble/a.iso"}},
		{LayoutReleases, "24.04/netboot/amd64/linux", []string{"24.04/netboot/amd64/linux"}},
		{LayoutReleases, "24.04/kubuntu/a.iso", nil},
		{LayoutCDImage, "24.04/ubuntu/a.iso", []string{"ubuntu/releases/24.04/release/a.iso"}},
		{LayoutCDImage, "noble/ubuntu-base/a.tar.gz", []string{"ubuntu-base/releases/noble/release/a.tar.gz"}},
		{LayoutCDImage, "noble/ubuntu-server/a.iso", []string{"ubuntu-server/noble/daily-live/current/a.iso"}},
		{LayoutCDImage, "noble/ubuntu-server/noble-preinstalled-server-arm64.img.xz", []string{"ubuntu-server/noble/daily-preinstalled/current/noble-preinstalled-server-arm64.img.xz"}},
		{LayoutCDImage, "noble/ubuntu-wsl/a.wsl", []string{"ubuntu-wsl/noble/daily-live/current/a.wsl"}},
		{LayoutCDImage, "24.04/ubuntu-core/a.img.xz", []string{"ubuntu-core/24/stable/current/a.img.xz"}},
		{LayoutCDImage, "focal/netboot/index.html", []string{"netboot/focal/index.html"}},
	}
	for _, tc := range cases {
		got := tc.layout.virtual(tc.canonical)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s virtual(%q) = %v; want %v", tc.layout, tc.canonical, got, tc.want)
		}
	}
}

func newLayoutView(t *testing.T, layout Layout) *View {
	t.Helper()
	content := map[string]string{
		"images/24.04/ubuntu/ubuntu-24.04.3-desktop-amd64.iso":           "desktop",
		"images/24.04/ubuntu/SHA256SUMS":                                 "sums",
		"images/noble/ubuntu/ubuntu-24.04.5-desktop-amd64.iso":           "noble-desktop",
		"images/noble/ubuntu-server/noble-live-server-amd64.iso":         "server",
		"images/24.04/ubuntu-core/ubuntu-core-24-amd64.img.xz":           "core",
		"images/24.04/netboot/amd64/linux":                               "kernel",
		"images/noble/ubuntu-base/ubuntu-base-24.04.3-base-amd64.tar.gz": "base",
		"images/24.04/kubuntu/kubuntu-24.04.5-desktop-amd64.iso":         "kubuntu",
	}
	objects := map[string][]storage.Object{}
	for name := range content {
		_, object, _ := strings.Cut(name, "/")
		objects["images"] = append(objects["images"], storage.Object{Name: object, Size: int64(len(content[name]))})
	}
	st := fakeStorage{objects: objects, content: content}
	idx := index.New()
	if err := idx.Refresh(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return NewLayout(context.Background(), idx, st, layout)
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
	if len(entries) != 2 || entries[0].Name() != "SHA256SUMS" || entries[1].Name() != "ubuntu-24.04.3-desktop-amd64.iso" {
		t.Fatalf("release entries = %v", entries)
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
	if got := readFile(t, v, "netboot/24.04/amd64/linux"); got != "kernel" {
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
		if !contains(names, want) {
			t.Errorf("24.04 entries = %v, missing %q", names, want)
		}
	}
	for _, unwanted := range []string{"kubuntu-24.04.5-desktop-amd64.iso"} {
		if contains(names, unwanted) {
			t.Errorf("24.04 entries = %v, unexpectedly contains %q", names, unwanted)
		}
	}
}

func TestViewImagesTree(t *testing.T) {
	v := newLayoutView(t, LayoutImages)
	if got := readFile(t, v, "24.04/ubuntu/ubuntu-24.04.3-desktop-amd64.iso"); got != "desktop" {
		t.Errorf("content = %q", got)
	}
	if got := readFile(t, v, "24.04/kubuntu/kubuntu-24.04.5-desktop-amd64.iso"); got != "kubuntu" {
		t.Errorf("kubuntu content = %q", got)
	}
}

func TestViewRootListing(t *testing.T) {
	cases := []struct {
		layout Layout
		want   []string
	}{
		{LayoutImages, []string{"24.04", "noble"}},
		{LayoutReleases, []string{"24.04", "noble"}},
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
	if want := "20.04,24.04"; strings.Join(got, ",") != want {
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

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
