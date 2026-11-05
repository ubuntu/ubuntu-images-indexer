package images

import (
	"slices"
	"strings"
	"testing"
)

// sample is a slice of the canonical tree as seeded by scripts/seed-swift.sh:
// per-series containers whose objects are named {release}/{product}/{path}.
var sample = []string{
	"noble 24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso",
	"noble 24.04.3/ubuntu/SHA256SUMS",
	"noble 24.04.5/kubuntu/kubuntu-24.04.5-desktop-amd64.iso",
	"noble 24.04/ubuntu-server/noble-live-server-amd64.iso",
	"noble 24.04/ubuntu-core/ubuntu-core-24-amd64.img.xz",
	"noble 24.04/ubuntu-wsl/noble-wsl-amd64.wsl",
	"noble beta/ubuntu/ubuntu-beta-desktop-amd64.iso",
	"resolute 26.04.1/ubuntu/ubuntu-26.04.1-desktop-amd64.iso",
	"resolute 26.04/ubuntu-base/ubuntu-base-26.04-base-amd64.tar.gz",
	"resolute 26.04/ubuntu-server/ubuntu-26.04-live-server-amd64.iso",
	"resolute 26.10/ubuntu/ubuntu-26.10-desktop-amd64.iso",
}

func newSampleCatalog(t *testing.T) *Catalog {
	t.Helper()
	var objects []Object
	for _, entry := range sample {
		container, name, _ := strings.Cut(entry, " ")
		o, ok := Parse(container, name)
		if !ok {
			t.Fatalf("Parse(%q, %q) failed", container, name)
		}
		objects = append(objects, o)
	}
	return NewCatalog(objects)
}

func TestCatalogResolve(t *testing.T) {
	c := newSampleCatalog(t)
	cases := []struct {
		alias string
		want  Series
		ok    bool
	}{
		{"noble", Series{24, 4}, true},
		{"resolute", Series{26, 4}, true}, // dominant series of the container
		{"24.04", Series{24, 4}, true},    // a series answers to its version too
		{"26.04", Series{26, 4}, true},
		{"26.10", Series{26, 10}, true},
		{"nope", Series{}, false},
	}
	for _, tc := range cases {
		got, ok := c.Resolve(tc.alias)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Resolve(%q) = %v, %v; want %v, %v", tc.alias, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCatalogResolveContainer(t *testing.T) {
	c := newSampleCatalog(t)
	if got, ok := c.ResolveContainer("noble"); !ok || got != (Series{24, 4}) {
		t.Errorf("ResolveContainer(noble) = %v, %v; want 24.04, true", got, ok)
	}
	// A bare version string names a series but not a container.
	if got, ok := c.ResolveContainer("24.04"); ok {
		t.Errorf("ResolveContainer(24.04) = %v, true; want false", got)
	}
}

func TestCatalogSeriesAndCodenames(t *testing.T) {
	c := newSampleCatalog(t)
	if got := c.Series(); !slices.Equal(got, []Series{{24, 4}, {26, 4}, {26, 10}}) {
		t.Errorf("Series() = %v; want [24.04 26.04 26.10]", got)
	}
	if got := c.Codenames(Series{24, 4}); !slices.Equal(got, []string{"noble"}) {
		t.Errorf("Codenames(24.04) = %v; want [noble]", got)
	}
	if got := c.Codenames(Series{26, 4}); !slices.Equal(got, []string{"resolute"}) {
		t.Errorf("Codenames(26.04) = %v; want [resolute]", got)
	}
	// The stray 26.10 object lives in the resolute container but is not
	// resolute's series, so it gets no codename.
	if got := c.Codenames(Series{26, 10}); len(got) != 0 {
		t.Errorf("Codenames(26.10) = %v; want none", got)
	}
}

func TestCatalogProducts(t *testing.T) {
	c := newSampleCatalog(t)
	if got := c.Products(Series{24, 4}); !slices.Equal(got, []string{"kubuntu", "ubuntu", "ubuntu-core", "ubuntu-server", "ubuntu-wsl"}) {
		t.Errorf("Products(24.04) = %v", got)
	}
	if got := c.Products(Series{26, 4}); !slices.Equal(got, []string{"ubuntu", "ubuntu-base", "ubuntu-server"}) {
		t.Errorf("Products(26.04) = %v", got)
	}
}

func TestCatalogLatest(t *testing.T) {
	c := newSampleCatalog(t)
	cases := []struct {
		series  Series
		product string
		want    Version
		ok      bool
	}{
		{Series{24, 4}, "ubuntu", Version{24, 4, 3}, true},
		{Series{24, 4}, "kubuntu", Version{24, 4, 5}, true},
		{Series{24, 4}, "ubuntu-server", Version{24, 4}, true},
		{Series{24, 4}, "ubuntu-wsl", Version{24, 4}, true},
		{Series{24, 4}, "nope", nil, false},
		{Series{26, 4}, "ubuntu", Version{26, 4, 1}, true},
		{Series{26, 10}, "ubuntu", Version{26, 10}, true},
	}
	for _, tc := range cases {
		got, ok := c.Latest(tc.series, tc.product)
		if ok != tc.ok || got.Compare(tc.want) != 0 {
			t.Errorf("Latest(%v, %q) = %v, %v; want %v, %v", tc.series, tc.product, got, ok, tc.want, tc.ok)
		}
	}
}

// TestNewCatalogNoVersionedSeries checks that a tree made only of milestone
// releases has no series and no latest.
func TestNewCatalogNoVersionedSeries(t *testing.T) {
	o, _ := Parse("noble", "beta/ubuntu/ubuntu-beta-desktop-amd64.iso")
	c := NewCatalog([]Object{o})
	if len(c.Series()) != 0 {
		t.Errorf("Series() = %v; want none", c.Series())
	}
	if _, ok := c.Latest(Series{}, "ubuntu"); ok {
		t.Error("Latest on an empty catalog = true; want false")
	}
}
