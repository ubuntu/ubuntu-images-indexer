package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

type fakeStorage struct {
	objects map[string][]storage.Object
}

func (f fakeStorage) Containers(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(f.objects))
	for name := range f.objects {
		names = append(names, name)
	}
	return names, nil
}

func (f fakeStorage) List(_ context.Context, container string, fn func(storage.Object) error) error {
	for _, o := range f.objects[container] {
		if err := fn(o); err != nil {
			return err
		}
	}
	return nil
}

func (f fakeStorage) URL(container, object string) string {
	return "https://store/" + container + "/" + object
}

func newServer(t *testing.T) *Server {
	t.Helper()
	st := fakeStorage{objects: map[string][]storage.Object{
		"images": {
			{Name: "a.txt", Size: 10},
			{Name: "dir/b.txt", Size: 2048},
		},
	}}
	idx := index.New()
	if err := idx.Refresh(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return New(idx, st)
}

func TestObjectRedirect(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/a.txt", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://store/images/a.txt" {
		t.Errorf("Location = %q", loc)
	}
}

func TestListing(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`href="https://store/images/a.txt"`, `href="/images/dir/"`, "10 B"} {
		if !strings.Contains(body, want) {
			t.Errorf("listing missing %q", want)
		}
	}
}

func TestSubdirListing(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/dir/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/images/"`) {
		t.Errorf("subdir listing missing parent link: %s", body)
	}
	if !strings.Contains(body, `href="https://store/images/dir/b.txt"`) {
		t.Errorf("subdir listing missing file link")
	}
}

func TestNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/images/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 512: "512 B", 1024: "1.0 KiB", 2048: "2.0 KiB", 1 << 20: "1.0 MiB"}
	for n, want := range cases {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}
