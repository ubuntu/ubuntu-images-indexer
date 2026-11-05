package images

import "testing"

func TestParseVersion(t *testing.T) {
	good := []string{
		"24.04",
		"24.04.3",
		"24.04.5.1",
		"26.10",
		"24",
		"9.04",
	}
	for _, in := range good {
		got, ok := ParseVersion(in)
		if !ok {
			t.Errorf("ParseVersion(%q) = false; want true", in)
			continue
		}
		if out := got.String(); out != in {
			t.Errorf("ParseVersion(%q).String() = %q; want %q", in, out, in)
		}
	}

	bad := []string{"", "24.", ".04", "24..4", "beta", "24.04a", "-1", "24.4.5.x"}
	for _, in := range bad {
		if got, ok := ParseVersion(in); ok {
			t.Errorf("ParseVersion(%q) = %v, true; want false", in, got)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b Version
		want int
	}{
		{Version{24, 4}, Version{24, 4}, 0},
		{Version{24, 4}, Version{24, 4, 0}, 0}, // missing component is zero
		{Version{24, 4}, Version{24, 4, 3}, -1},
		{Version{24, 4, 3}, Version{24, 4, 5}, -1},
		{Version{24, 4, 5}, Version{24, 4, 5, 1}, -1},
		{Version{26, 4}, Version{24, 4}, 1},
		{Version{26, 10}, Version{26, 4}, 1},
	}
	for _, tc := range cases {
		if got := tc.a.Compare(tc.b); got != tc.want {
			t.Errorf("%v.Compare(%v) = %d; want %d", tc.a, tc.b, got, tc.want)
		}
		if got := tc.b.Compare(tc.a); got != -tc.want {
			t.Errorf("%v.Compare(%v) = %d; want %d (inverse)", tc.b, tc.a, got, -tc.want)
		}
	}
}

func TestVersionSeries(t *testing.T) {
	cases := []struct {
		in   Version
		want Series
		ok   bool
	}{
		{Version{24, 4, 3}, Series{24, 4}, true},
		{Version{26, 10}, Series{26, 10}, true},
		{Version{24}, Series{}, false},
		{Version{}, Series{}, false},
	}
	for _, tc := range cases {
		got, ok := tc.in.Series()
		if ok != tc.ok || got != tc.want {
			t.Errorf("%v.Series() = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	if got := (Series{24, 4}).String(); got != "24.04" {
		t.Errorf("Series{24,4}.String() = %q; want %q", got, "24.04")
	}
	if got := (Series{26, 10}).String(); got != "26.10" {
		t.Errorf("Series{26,10}.String() = %q; want %q", got, "26.10")
	}
}

func TestParseRelease(t *testing.T) {
	cases := []struct {
		in        string
		version   Version
		milestone string
		ok        bool
	}{
		{"24.04", Version{24, 4}, "", true},
		{"24.04.3", Version{24, 4, 3}, "", true},
		{"beta", nil, "beta", true},
		{"", nil, "", false},
		{"a/b", nil, "", false},
	}
	for _, tc := range cases {
		got, ok := ParseRelease(tc.in)
		if ok != tc.ok {
			t.Errorf("ParseRelease(%q) ok = %v; want %v", tc.in, ok, tc.ok)
			continue
		}
		if !tc.ok {
			continue
		}
		if got.Milestone != tc.milestone || got.Version.Compare(tc.version) != 0 {
			t.Errorf("ParseRelease(%q) = %+v; want version %v milestone %q", tc.in, got, tc.version, tc.milestone)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		container string
		name      string
		release   string
		product   string
		path      string
		ok        bool
	}{
		{"noble", "24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso", "24.04.3", "ubuntu", "ubuntu-24.04.3-desktop-amd64.iso", true},
		{"noble", "24.04/ubuntu-wsl/noble-wsl-amd64.wsl", "24.04", "ubuntu-wsl", "noble-wsl-amd64.wsl", true},
		{"noble", "24.04/ubuntu/SHA256SUMS", "24.04", "ubuntu", "SHA256SUMS", true},
		{"noble", "beta/ubuntu/ubuntu-beta-desktop-amd64.iso", "beta", "ubuntu", "ubuntu-beta-desktop-amd64.iso", true},
		{"resolute", "26.04.1/ubuntu/ubuntu-26.04.1-desktop-amd64.iso", "26.04.1", "ubuntu", "ubuntu-26.04.1-desktop-amd64.iso", true},
		{"resolute", "26.10/ubuntu/ubuntu-26.10-desktop-amd64.iso", "26.10", "ubuntu", "ubuntu-26.10-desktop-amd64.iso", true},
		{"noble", "HEADER.html", "", "", "", false},
		{"noble", "", "", "", "", false},
		{"noble", "24.04", "", "", "", false},
		{"noble", "/ubuntu/a.iso", "", "", "", false},
	}
	for _, tc := range cases {
		got, ok := Parse(tc.container, tc.name)
		if ok != tc.ok {
			t.Errorf("Parse(%q, %q) ok = %v; want %v", tc.container, tc.name, ok, tc.ok)
			continue
		}
		if !tc.ok {
			continue
		}
		if got.Container != tc.container || got.Product != tc.product || got.Path != tc.path || got.Release.String() != tc.release {
			t.Errorf("Parse(%q, %q) = %+v; want container %q release %q product %q path %q", tc.container, tc.name, got, tc.container, tc.release, tc.product, tc.path)
		}
	}
}

func TestObjectSeries(t *testing.T) {
	versioned, _ := Parse("noble", "24.04.3/ubuntu/a.iso")
	if s, ok := versioned.Series(); !ok || s != (Series{24, 4}) {
		t.Errorf("versioned.Series() = %v, %v; want {24 4}, true", s, ok)
	}
	milestone, _ := Parse("noble", "beta/ubuntu/a.iso")
	if s, ok := milestone.Series(); ok {
		t.Errorf("milestone.Series() = %v, true; want false", s)
	}
}
