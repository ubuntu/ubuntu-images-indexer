// Package config loads and validates the ubimi configuration file.
package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/ubuntu/ubimi/internal/storage"
	"github.com/ubuntu/ubimi/internal/vfs"
)

// Config is the top-level ubimi configuration.
type Config struct {
	Listen  string        `toml:"listen"`
	Storage StorageConfig `toml:"storage"`
	Index   IndexConfig   `toml:"index"`
	Rsync   RsyncConfig   `toml:"rsync"`
	// Layouts maps a request hostname to the tree layout the HTTP listener
	// exposes for it (cdimage, releases or images). Hosts not listed fall back
	// to the images layout, whose first path segment selects the container.
	Layouts map[string]string `toml:"layouts"`
}

// RsyncConfig enables an additional rsync daemon listener. Modules named after
// a known layout (see vfs.Layouts) expose the canonical images tree through
// that layout. Other module names expose the containers directly.
type RsyncConfig struct {
	Listen  string   `toml:"listen"`  // empty disables the listener
	Modules []string `toml:"modules"` // defaults to the known layouts
	Module  string   `toml:"module"`  // deprecated single module, added to Modules
}

// ModuleNames returns the configured modules in order, or the known layouts
// when none are configured.
func (c RsyncConfig) ModuleNames() []string {
	names := slices.Clone(c.Modules)
	if len(names) == 0 {
		for _, l := range vfs.Layouts() {
			names = append(names, string(l))
		}
	}
	if c.Module != "" && !slices.Contains(names, c.Module) {
		names = append(names, c.Module)
	}
	return names
}

// StorageConfig describes how to reach the Swift/RadosGW object store.
type StorageConfig struct {
	// AuthVersion selects the identity (Keystone) API version: 1, 2 or 3.
	// Zero (the default) lets the client infer it from AuthURL.
	AuthVersion   int    `toml:"auth_version"`
	AuthURL       string `toml:"auth_url"`
	User          string `toml:"user"`
	Password      string `toml:"password"`
	UserDomain    string `toml:"user_domain"`
	Project       string `toml:"project"`
	ProjectDomain string `toml:"project_domain"`
	Region        string `toml:"region"`
	// PublicURL is the externally reachable base used to build listing links
	// and redirects. It is required and never taken from the Keystone catalog.
	PublicURL string `toml:"public_url"`
}

// Options returns the storage options the configured backend is built from.
func (s StorageConfig) Options() storage.Options {
	return storage.Options{
		AuthVersion:   s.AuthVersion,
		AuthURL:       s.AuthURL,
		User:          s.User,
		Password:      s.Password,
		UserDomain:    s.UserDomain,
		Project:       s.Project,
		ProjectDomain: s.ProjectDomain,
		Region:        s.Region,
		PublicURL:     s.PublicURL,
	}
}

// effectiveAuthVersion returns the identity API version in use: an explicit
// AuthVersion wins, otherwise it is inferred from AuthURL the same way the
// swift client does. Zero means it could not be determined.
func (s StorageConfig) effectiveAuthVersion() int {
	if s.AuthVersion != 0 {
		return s.AuthVersion
	}
	switch {
	case strings.Contains(s.AuthURL, "v3"):
		return 3
	case strings.Contains(s.AuthURL, "v2"):
		return 2
	case strings.Contains(s.AuthURL, "v1"):
		return 1
	default:
		return 0
	}
}

// IndexConfig controls the background crawl of the object store.
type IndexConfig struct {
	RefreshInterval Duration `toml:"refresh_interval"`
}

// Duration is a time.Duration that unmarshals from a TOML string such as "5m".
type Duration time.Duration

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Load reads, defaults and validates the configuration at path. Unknown keys
// are rejected so that a stale or misspelled section (a leftover [[route]]
// table, say) is a load error rather than being silently ignored.
func Load(path string) (*Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("invalid config %s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.Index.RefreshInterval == 0 {
		c.Index.RefreshInterval = Duration(5 * time.Minute)
	}
	c.Storage.PublicURL = strings.TrimRight(c.Storage.PublicURL, "/")
}

func (c *Config) validate() error {
	s := c.Storage
	switch {
	case s.AuthURL == "":
		return fmt.Errorf("storage.auth_url is required")
	case s.User == "":
		return fmt.Errorf("storage.user is required")
	case s.Password == "":
		return fmt.Errorf("storage.password is required")
	case s.AuthVersion < 0 || s.AuthVersion > 3:
		return fmt.Errorf("storage.auth_version %d is invalid, want 1, 2 or 3 (or unset to infer it from auth_url)", s.AuthVersion)
	case s.PublicURL == "":
		return fmt.Errorf("storage.public_url is required")
	}
	// v2 and v3 authenticate against a project (tenant); v1 does not.
	if v := s.effectiveAuthVersion(); (v == 2 || v == 3) && s.Project == "" {
		return fmt.Errorf("storage.project is required for auth version %d", v)
	}
	if u, err := url.Parse(s.PublicURL); err != nil || !u.IsAbs() {
		return fmt.Errorf("storage.public_url %q must be an absolute URL", s.PublicURL)
	}
	for host, layout := range c.Layouts {
		// The layout names are the vfs layouts, shared with the rsync modules.
		if _, ok := vfs.ParseLayout(layout); !ok {
			return fmt.Errorf("layouts: host %q maps to unknown layout %q", host, layout)
		}
	}
	return nil
}
