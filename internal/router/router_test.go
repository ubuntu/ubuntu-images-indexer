package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/ubuntu/ubimi/internal/config"
)

func TestMatchHost(t *testing.T) {
	cases := []struct {
		pattern, host string
		want          bool
	}{
		{"", "anything", true},
		{"images.example.com", "images.example.com", true},
		{"images.example.com", "other.example.com", false},
		{"images.example.com", "images.example.com:8080", true},
		{"*.example.com", "a.example.com", true},
		{"*.example.com", "a.example.com:80", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "badexample.com", false},
	}
	for _, c := range cases {
		if got := matchHost(c.pattern, c.host); got != c.want {
			t.Errorf("matchHost(%q, %q) = %v, want %v", c.pattern, c.host, got, c.want)
		}
	}
}

func TestMatchPath(t *testing.T) {
	cases := []struct {
		prefix, path string
		want         bool
	}{
		{"", "/x", true},
		{"/", "/x", true},
		{"/legacy/", "/legacy/a", true},
		{"/legacy/", "/other", false},
		{"/img", "/img", true},
		{"/img", "/img/a", true},
		{"/img", "/images", false},
	}
	for _, c := range cases {
		if got := matchPath(c.prefix, c.path); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.prefix, c.path, got, c.want)
		}
	}
}

func TestExpand(t *testing.T) {
	u, _ := url.Parse("/legacy/a/b%20c?x=1")
	got := expand("https://images.example.com/{container}/{path}", "/legacy/", u)
	if want := "https://images.example.com/a/a/b%20c"; got != want {
		t.Errorf("expand = %q, want %q", got, want)
	}
}

func TestHandlerRedirectAndFallthrough(t *testing.T) {
	rules := []config.Route{{Path: "/legacy/", Target: "https://images.example.com/{path}", Status: 301}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := Handler(rules, next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/legacy/foo/bar", nil))
	if rec.Code != 301 {
		t.Errorf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://images.example.com/foo/bar" {
		t.Errorf("Location = %q", loc)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/other", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("fallthrough status = %d, want 418", rec.Code)
	}
}

func TestRegexRedirect(t *testing.T) {
	rules := []config.Route{{
		Host:   "cdimage.ubuntu.com",
		Match:  `^/(?P<product>[^/]+)/releases/(?P<series>[^/]+)/?$`,
		Regexp: regexp.MustCompile(`^/(?P<product>[^/]+)/releases/(?P<series>[^/]+)/?$`),
		Target: "https://images.ubuntu.com/images/{series}/{product}",
		Status: 302,
	}}
	h := Handler(rules, http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/ubuntu/releases/24.04", nil)
	req.Host = "cdimage.ubuntu.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://images.ubuntu.com/images/24.04/ubuntu" {
		t.Errorf("Location = %q", loc)
	}

	// Wrong host must not match the rule.
	req = httptest.NewRequest(http.MethodGet, "/ubuntu/releases/24.04", nil)
	req.Host = "other.example.com"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("host mismatch status = %d, want 404", rec.Code)
	}
}

func TestRedirectCarriesQuery(t *testing.T) {
	rules := []config.Route{{
		Match:  `^/get/(?P<name>[^/]+)$`,
		Regexp: regexp.MustCompile(`^/get/(?P<name>[^/]+)$`),
		Target: "https://images.example.com/{name}",
		Status: 302,
	}}
	rec := httptest.NewRecorder()
	Handler(rules, http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/get/a?x=1&y=2", nil))
	if loc := rec.Header().Get("Location"); loc != "https://images.example.com/a?x=1&y=2" {
		t.Errorf("Location = %q", loc)
	}
}
