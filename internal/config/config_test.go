package config

import (
	"os"
	"path/filepath"
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

func TestLoadRouteDefaultsStatus(t *testing.T) {
	cfg, err := Load(write(t, minimal+`
[[route]]
path = "/legacy/"
target = "https://images.example.com/{path}"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].Status != 302 {
		t.Fatalf("routes = %+v, want one with status 302", cfg.Routes)
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
		"route without target": minimal + `
[[route]]
path = "/x/"
`,
		"invalid regex match": minimal + `
[[route]]
match = "["
target = "https://images.example.com/{1}"
`,
		"both path and match": minimal + `
[[route]]
path = "/x/"
match = "^/x$"
target = "https://images.example.com/"
`,
		"neither path nor match": minimal + `
[[route]]
target = "https://images.example.com/"
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

func TestLoadCompilesRegexRoute(t *testing.T) {
	cfg, err := Load(write(t, minimal+`
[[route]]
host   = "cdimage.ubuntu.com"
match  = "^/(?P<product>[^/]+)/releases/(?P<series>[^/]+)/?$"
target = "https://images.ubuntu.com/images/{series}/{product}"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(cfg.Routes))
	}
	if cfg.Routes[0].Regexp == nil {
		t.Fatal("route Regexp was not compiled")
	}
	if cfg.Routes[0].Status != 302 {
		t.Errorf("status = %d, want default 302", cfg.Routes[0].Status)
	}
}
