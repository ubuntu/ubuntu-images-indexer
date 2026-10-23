// Package router implements the host+URI redirect farm used to migrate legacy
// URLs onto the current object-store layout.
package router

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ubuntu/ubimi/internal/config"
)

// Handler returns an http.Handler that applies the redirect rules in order and
// falls through to next when none match.
func Handler(rules []config.Route, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rule := range rules {
			if !matchHost(rule.Host, r.Host) || !matchPath(rule.Path, r.URL.Path) {
				continue
			}
			target := expand(rule.Target, rule.Path, r.URL)
			http.Redirect(w, r, target, rule.Status)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// matchHost matches an exact hostname, a "*.suffix" wildcard, or any host when
// the pattern is empty. A port in the request host is ignored for wildcards.
func matchHost(pattern, host string) bool {
	if pattern == "" {
		return true
	}
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	if suffix, ok := strings.CutPrefix(pattern, "*"); ok {
		return strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix))
	}
	return strings.EqualFold(pattern, name) || strings.EqualFold(pattern, host)
}

// matchPath matches a URI path prefix on a segment boundary.
func matchPath(prefix, path string) bool {
	if prefix == "" {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return strings.HasSuffix(prefix, "/") || len(path) == len(prefix) || path[len(prefix)] == '/'
}

// expand fills {path} with the URI remainder and {container} with its first
// segment, and carries over any query string. The path is kept in its escaped
// form so that reserved characters survive the redirect.
func expand(target, prefix string, u *url.URL) string {
	remainder := strings.TrimPrefix(u.EscapedPath(), prefix)
	out := strings.ReplaceAll(target, "{path}", remainder)
	container, _, _ := strings.Cut(remainder, "/")
	out = strings.ReplaceAll(out, "{container}", container)
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}
