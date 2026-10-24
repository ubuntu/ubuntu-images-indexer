package storage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ncw/swift/v2"

	"github.com/ubuntu/ubimi/internal/config"
)

func TestURL(t *testing.T) {
	s := New(config.StorageConfig{PublicURL: "https://images.example.com/"})
	if got, want := s.URL("images", "a b/c.txt"), "https://images.example.com/images/a%20b/c.txt"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestToObject(t *testing.T) {
	modified := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	o := toObject(swift.Object{
		Name:         "a.txt",
		Bytes:        42,
		ContentType:  "text/plain",
		LastModified: modified,
		Hash:         "abc",
	})
	if o.Name != "a.txt" || o.Size != 42 || o.ContentType != "text/plain" || o.ETag != "abc" {
		t.Errorf("toObject = %+v", o)
	}
	if !o.LastModified.Equal(modified) {
		t.Errorf("LastModified = %v, want %v", o.LastModified, modified)
	}
}

const testContent = "hello world"

// newTestServer starts a fake identity (Keystone v1, v2 and v3) and Swift
// endpoint and returns its base URL. The Swift side serves one container
// "images" containing "a.txt", guarded by the token all three auth flows issue.
func newTestServer(t *testing.T) string {
	t.Helper()
	const token = "tok"
	var base string
	mux := http.NewServeMux()

	// v3: POST {auth}/auth/tokens, token in the X-Subject-Token header.
	mux.HandleFunc("/v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("auth method = %s, want POST", r.Method)
		}
		w.Header().Set("X-Subject-Token", token)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"token": map[string]any{
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"catalog": []any{map[string]any{
				"type": "object-store",
				"endpoints": []any{map[string]any{
					"interface": "public", "region": "RegionOne", "url": base + "/swift",
				}},
			}},
		}})
	})
	// v2: POST {auth}/tokens returning a service catalog.
	mux.HandleFunc("/v2.0/tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("auth method = %s, want POST", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"access": map[string]any{
			"token": map[string]any{
				"id":      token,
				"expires": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			},
			"serviceCatalog": []any{map[string]any{
				"type": "object-store",
				"endpoints": []any{map[string]any{
					"publicURL": base + "/swift", "region": "RegionOne",
				}},
			}},
		}})
	})
	// v1: GET {auth}, storage URL and token in the response headers.
	mux.HandleFunc("/v1.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Storage-Url", base+"/swift")
		w.Header().Set("X-Auth-Token", token)
	})

	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("X-Auth-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/swift", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		// Account listings are newline-delimited container names.
		io.WriteString(w, "images\nother\n")
	})
	mux.HandleFunc("/swift/images", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"name": "a.txt", "bytes": len(testContent), "content_type": "text/plain", "last_modified": "2024-01-02T03:04:05.000000", "hash": "h"},
		})
	})
	mux.HandleFunc("/swift/images/a.txt", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		http.ServeContent(w, r, "a.txt", time.Time{}, strings.NewReader(testContent))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return base
}

// newTestSwift returns a v3-authenticated Swift storage pointed at a fresh fake
// identity and Swift endpoint.
func newTestSwift(t *testing.T) *Swift {
	return New(config.StorageConfig{
		AuthURL:   newTestServer(t) + "/v3",
		User:      "u",
		Password:  "p",
		Project:   "pr",
		PublicURL: "https://pub",
	})
}

// TestAuthVersions checks that auth_version is passed through to the client and
// that the v1, v2 and v3 flows all authenticate and list containers.
func TestAuthVersions(t *testing.T) {
	cases := map[string]struct {
		version int
		path    string
	}{
		"v1 explicit":         {1, "/v1.0"},
		"v2 explicit":         {2, "/v2.0"},
		"v3 explicit":         {3, "/v3"},
		"autodetect from url": {0, "/v2.0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := New(config.StorageConfig{
				AuthVersion: tc.version,
				AuthURL:     newTestServer(t) + tc.path,
				User:        "u",
				Password:    "p",
				Project:     "pr",
				PublicURL:   "https://pub",
			})
			containers, err := s.Containers(context.Background())
			if err != nil {
				t.Fatalf("Containers: %v", err)
			}
			if len(containers) != 2 || containers[0] != "images" || containers[1] != "other" {
				t.Errorf("containers = %v, want [images other]", containers)
			}
		})
	}
}

// TestAuthDiscoveryAndListing exercises token issuance, catalog discovery,
// container enumeration and object listing.
func TestAuthDiscoveryAndListing(t *testing.T) {
	s := newTestSwift(t)
	ctx := context.Background()

	containers, err := s.Containers(ctx)
	if err != nil {
		t.Fatalf("Containers: %v", err)
	}
	if len(containers) != 2 || containers[0] != "images" || containers[1] != "other" {
		t.Errorf("containers = %v, want [images other]", containers)
	}

	var got []Object
	if err := s.List(ctx, "images", func(o Object) error { got = append(got, o); return nil }); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a.txt" || got[0].Size != int64(len(testContent)) {
		t.Errorf("objects = %+v, want one a.txt of size %d", got, len(testContent))
	}
}

func TestOpen(t *testing.T) {
	s := newTestSwift(t)
	ctx := context.Background()

	rc, err := s.Open(ctx, "images", "a.txt", 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != testContent {
		t.Errorf("content = %q, want %q", got, testContent)
	}

	// A non-zero offset issues a range request.
	rc, err = s.Open(ctx, "images", "a.txt", 6)
	if err != nil {
		t.Fatalf("Open offset: %v", err)
	}
	got, err = io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("ReadAll offset: %v", err)
	}
	if string(got) != "world" {
		t.Errorf("offset content = %q, want %q", got, "world")
	}
}
