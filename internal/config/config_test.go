package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimal = `
[storage]
auth_url = "https://keystone.example.com/v3"
user = "ubimi"
password = "secret"
project = "images"
public_url = "https://images.example.com/"
`

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(write(t, minimal))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want :8080", cfg.Listen)
	}
	if got := time.Duration(cfg.Index.RefreshInterval); got != 5*time.Minute {
		t.Errorf("RefreshInterval = %v, want 5m", got)
	}
	if cfg.Storage.PublicURL != "https://images.example.com" {
		t.Errorf("PublicURL = %q, want trailing slash trimmed", cfg.Storage.PublicURL)
	}
}

func TestLoadLayouts(t *testing.T) {
	cfg, err := Load(write(t, minimal+`
[layouts]
"cdimage.ubuntu.com" = "cdimage"
"releases.ubuntu.com" = "releases"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Layouts["cdimage.ubuntu.com"]; got != "cdimage" {
		t.Errorf("cdimage.ubuntu.com = %q, want cdimage", got)
	}
	if got := cfg.Layouts["releases.ubuntu.com"]; got != "releases" {
		t.Errorf("releases.ubuntu.com = %q, want releases", got)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := map[string]string{
		"missing public_url": `
[storage]
auth_url = "https://k/v3"
user = "u"
password = "p"
project = "pr"
`,
		"relative public_url": `
[storage]
auth_url = "https://k/v3"
user = "u"
password = "p"
project = "pr"
public_url = "images.example.com"
`,
		"unknown layout": minimal + `
[layouts]
"example.com" = "nope"
`,
	}
	for name, body := range cases {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: Load succeeded, want error", name)
		}
	}
}

func TestLoadAuthVersions(t *testing.T) {
	cases := map[string]struct {
		body  string
		valid bool
	}{
		"v1 needs no project": {`
[storage]
auth_url = "https://identity.example.com/v1.0"
user = "u"
password = "p"
public_url = "https://images.example.com"
`, true},
		"v2 needs project": {`
[storage]
auth_url = "https://identity.example.com/v2.0"
user = "u"
password = "p"
public_url = "https://images.example.com"
`, false},
		"explicit v1 needs no project": {`
[storage]
auth_version = 1
auth_url = "https://identity.example.com/"
user = "u"
password = "p"
public_url = "https://images.example.com"
`, true},
		"explicit v3 needs project": {`
[storage]
auth_version = 3
auth_url = "https://identity.example.com/"
user = "u"
password = "p"
public_url = "https://images.example.com"
`, false},
		"invalid auth_version": {`
[storage]
auth_version = 4
auth_url = "https://identity.example.com/v3"
user = "u"
password = "p"
project = "pr"
public_url = "https://images.example.com"
`, false},
	}
	for name, tc := range cases {
		_, err := Load(write(t, tc.body))
		if tc.valid && err != nil {
			t.Errorf("%s: Load: %v", name, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("%s: Load succeeded, want error", name)
		}
	}
}

func TestRsyncModuleNames(t *testing.T) {
	cases := []struct {
		name string
		cfg  RsyncConfig
		want []string
	}{
		{"default layouts", RsyncConfig{}, []string{"cdimage", "releases", "images"}},
		{"explicit modules", RsyncConfig{Modules: []string{"images", "legacy"}}, []string{"images", "legacy"}},
		{"legacy module added", RsyncConfig{Module: "ubimi"}, []string{"cdimage", "releases", "images", "ubimi"}},
		{"legacy module not duplicated", RsyncConfig{Modules: []string{"releases"}, Module: "releases"}, []string{"releases"}},
	}
	for _, tc := range cases {
		if got := tc.cfg.ModuleNames(); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: ModuleNames() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
