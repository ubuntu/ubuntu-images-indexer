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

	"github.com/ubuntu/ubimi/internal/config"
)

func TestURL(t *testing.T) {
	s := New(config.StorageConfig{PublicURL: "https://images.example.com/"})
	if got, want := s.URL("images", "a b/c.txt"), "https://images.example.com/images/a%20b/c.txt"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestToObject(t *testing.T) {
	o := swiftObject{
		Name:         "a.txt",
		Bytes:        42,
		ContentType:  "text/plain",
		LastModified: "2024-01-02T03:04:05.000000",
		Hash:         "abc",
	}.toObject()
	if o.Name != "a.txt" || o.Size != 42 || o.ContentType != "text/plain" || o.ETag != "abc" {
		t.Errorf("toObject = %+v", o)
	}
	if o.LastModified.IsZero() {
		t.Error("LastModified not parsed")
	}
}

const testContent = "hello world"

// newTestSwift starts a fake Keystone and Swift endpoint and returns a Swift
// storage pointed at them. The fake serves one container "images" containing
// "a.txt".
func newTestSwift(t *testing.T) *Swift {
	t.Helper()
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("auth method = %s, want POST", r.Method)
		}
		w.Header().Set("X-Subject-Token", "tok")
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
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("X-Auth-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/swift", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{{"name": "images"}, {"name": "other"}})
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

	return New(config.StorageConfig{AuthURL: base + "/v3", User: "u", Password: "p", Project: "pr", PublicURL: "https://pub"})
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
