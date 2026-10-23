package storage

import (
	"testing"

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
