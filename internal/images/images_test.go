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

func TestParseArchitecture(t *testing.T) {
	good := []struct {
		path string
		want string
	}{
		{"ubuntu_desktop_24.04.3_amd64_20240418.iso", "amd64"},
		{"ubuntu_desktop_24.04.3_arm64_20240418.iso.sha256", "arm64"},
		{"ubuntu_desktop_24.04.3_ppc64el_20240418.iso.sha256.gpg", "ppc64el"},
		{"ubuntu_base_24.04.3_arm64_20240418.tar.gz", "arm64"},
		{"ubuntu_core_24_amd64_20240419.img.xz", "amd64"},
		{"ubuntu_preinstalled-server_24.04_arm64_20240418.img.xz", "arm64"},
		{"kubuntu_desktop_24.04.5_amd64_20240418.iso.zsync", "amd64"},
		{"24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso", "amd64"},
	}
	for _, tc := range good {
		got, ok := ParseArchitecture(tc.path)
		if !ok || got != tc.want {
			t.Errorf("ParseArchitecture(%q) = %q, %v; want %q, true", tc.path, got, ok, tc.want)
		}
	}

	bad := []string{
		"",
		"SHA256SUMS",
		"HEADER.html",
		"linux",
		"amd64/linux",
		"24.04/ubuntu",
		"ubuntu_desktop_24.04.3_amd64.iso",    // no build date
		"ubuntu_desktop_24.04.3_20240418.iso", // no architecture
		"ubuntu_desktop_24.04.3_amd64_2024041.iso", // truncated date
		"ubuntu_desktop_24.04.3_AMD64_20240418.iso",
		"ubuntu_desktop_24.04.3_64_20240418.iso",
	}
	for _, path := range bad {
		if got, ok := ParseArchitecture(path); ok {
			t.Errorf("ParseArchitecture(%q) = %q, true; want false", path, got)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		container string
		name      string
		release   string
		product   string
		arch      string
		path      string
		ok        bool
	}{
		{"noble", "24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso", "24.04.3", "ubuntu", "amd64", "ubuntu_desktop_24.04.3_amd64_20240418.iso", true},
		{"noble", "24.04.3/ubuntu/ubuntu_desktop_24.04.3_arm64_20240418.iso.sha256", "24.04.3", "ubuntu", "arm64", "ubuntu_desktop_24.04.3_arm64_20240418.iso.sha256", true},
		{"noble", "24.04/ubuntu-wsl/ubuntu_wsl_24.04_amd64_20240418.wsl", "24.04", "ubuntu-wsl", "amd64", "ubuntu_wsl_24.04_amd64_20240418.wsl", true},
		{"noble", "24.04/ubuntu/SHA256SUMS", "24.04", "ubuntu", "", "SHA256SUMS", true},
		{"noble", "24.04-beta/ubuntu/ubuntu_desktop_24.04-beta_amd64_20240322.iso", "24.04-beta", "ubuntu", "amd64", "ubuntu_desktop_24.04-beta_amd64_20240322.iso", true},
		{"resolute", "26.04.1/ubuntu/ubuntu_desktop_26.04.1_amd64_20260418.iso", "26.04.1", "ubuntu", "amd64", "ubuntu_desktop_26.04.1_amd64_20260418.iso", true},
		{"resolute", "26.04/ubuntu-base/ubuntu_base_26.04_arm64_20260418.tar.gz", "26.04", "ubuntu-base", "arm64", "ubuntu_base_26.04_arm64_20260418.tar.gz", true},
		{"stonking", "26.10/ubuntu/ubuntu_desktop_26.10_amd64_20260418.iso", "26.10", "ubuntu", "amd64", "ubuntu_desktop_26.10_amd64_20260418.iso", true},
		{"noble", "HEADER.html", "", "", "", "", false},
		{"noble", "", "", "", "", "", false},
		{"noble", "24.04", "", "", "", "", false},
		{"noble", "/ubuntu/a.iso", "", "", "", "", false},
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
		if got.Container != tc.container || got.Product != tc.product || got.Architecture != tc.arch || got.Path != tc.path || got.Release.String() != tc.release {
			t.Errorf("Parse(%q, %q) = %+v; want container %q release %q product %q architecture %q path %q", tc.container, tc.name, got, tc.container, tc.release, tc.product, tc.arch, tc.path)
		}
	}
}

func TestObjectSeries(t *testing.T) {
	versioned, _ := Parse("noble", "24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso")
	if s, ok := versioned.Series(); !ok || s != (Series{24, 4}) {
		t.Errorf("versioned.Series() = %v, %v; want {24 4}, true", s, ok)
	}
	milestone, _ := Parse("noble", "24.04-beta/ubuntu/ubuntu_desktop_24.04-beta_amd64_20240322.iso")
	if s, ok := milestone.Series(); ok {
		t.Errorf("milestone.Series() = %v, true; want false", s)
	}
}
