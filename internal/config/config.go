// Package config loads and validates the ubimi configuration file.
package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the top-level ubimi configuration.
type Config struct {
	Listen  string        `toml:"listen"`
	Storage StorageConfig `toml:"storage"`
	Index   IndexConfig   `toml:"index"`
	Rsync   RsyncConfig   `toml:"rsync"`
	Routes  []Route       `toml:"route"`
}

// RsyncConfig enables an additional rsync daemon listener exposing the indexed
// object store as a single module.
type RsyncConfig struct {
	Listen string `toml:"listen"` // empty disables the listener
	Module string `toml:"module"` // module name, defaults to "ubimi"
}

// StorageConfig describes how to reach the Swift/RadosGW object store.
type StorageConfig struct {
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

// IndexConfig controls the background crawl of the object store.
type IndexConfig struct {
	RefreshInterval Duration `toml:"refresh_interval"`
}

// Route redirects requests matching a host and either a path prefix (Path) or
// a regular expression (Match) to a target URL. Exactly one of Path or Match
// must be set.
//
// For prefix routes, Target may reference {path} (the URI remainder after the
// prefix) and {container} (the first path segment). For regex routes, Match is
// compiled with named capture groups that Target may reference as {name}.
type Route struct {
	Host   string `toml:"host"`
	Path   string `toml:"path"`
	Match  string `toml:"match"`
	Target string `toml:"target"`
	Status int    `toml:"status"`

	// Regexp is the compiled form of Match, populated by Load.
	Regexp *regexp.Regexp `toml:"-"`
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

// Load reads, defaults and validates the configuration at path.
func Load(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
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
	if c.Rsync.Listen != "" && c.Rsync.Module == "" {
		c.Rsync.Module = "ubimi"
	}
	for i := range c.Routes {
		if c.Routes[i].Status == 0 {
			c.Routes[i].Status = 302
		}
	}
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
	case s.Project == "":
		return fmt.Errorf("storage.project is required")
	case s.PublicURL == "":
		return fmt.Errorf("storage.public_url is required")
	}
	if u, err := url.Parse(s.PublicURL); err != nil || !u.IsAbs() {
		return fmt.Errorf("storage.public_url %q must be an absolute URL", s.PublicURL)
	}
	for i := range c.Routes {
		r := &c.Routes[i]
		switch {
		case r.Target == "":
			return fmt.Errorf("route %d: target is required", i)
		case r.Path == "" && r.Match == "":
			return fmt.Errorf("route %d: one of path or match is required", i)
		case r.Path != "" && r.Match != "":
			return fmt.Errorf("route %d: path and match are mutually exclusive", i)
		}
		if r.Match != "" {
			re, err := regexp.Compile(r.Match)
			if err != nil {
				return fmt.Errorf("route %d: invalid match: %w", i, err)
			}
			r.Regexp = re
		}
	}
	return nil
}
