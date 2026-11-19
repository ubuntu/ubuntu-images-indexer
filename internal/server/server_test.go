package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
	"github.com/ubuntu/ubimi/internal/vfs"
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

func (f fakeStorage) Open(context.Context, string, string, int64) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func newServer(t *testing.T, st storage.Storage, layouts map[string]vfs.Layout) *Server {
	t.Helper()
	idx := index.New()
	if err := idx.Refresh(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return New(st, layouts, vfs.NewViews(context.Background(), idx, st))
}

func TestObjectRedirect(t *testing.T) {
	st := fakeStorage{objects: map[string][]storage.Object{
		"images": {
			{Name: "a.txt", Size: 10},
			{Name: "dir/b.txt", Size: 2048},
		},
	}}
	rec := httptest.NewRecorder()
	newServer(t, st, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/a.txt", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://store/images/a.txt" {
		t.Errorf("Location = %q", loc)
	}
}

func TestListing(t *testing.T) {
	st := fakeStorage{objects: map[string][]storage.Object{
		"images": {
			{Name: "a.txt", Size: 10},
			{Name: "dir/b.txt", Size: 2048},
		},
	}}
	rec := httptest.NewRecorder()
	newServer(t, st, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/", nil))
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
	st := fakeStorage{objects: map[string][]storage.Object{
		"images": {
			{Name: "a.txt", Size: 10},
			{Name: "dir/b.txt", Size: 2048},
		},
	}}
	rec := httptest.NewRecorder()
	newServer(t, st, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/dir/", nil))
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

// TestLatestRedirect checks the virtual "latest" permalink: files redirect
// straight to the object store and the latest folder stays virtual so the
// codename-based aliases remain visible only there.
func TestLatestRedirect(t *testing.T) {
	st := fakeStorage{objects: map[string][]storage.Object{
		"noble": {
			{Name: "24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso", Size: 10},
			{Name: "24.04.5/ubuntu/ubuntu_desktop_24.04.5_amd64_20240418.iso.sha256", Size: 10},
			{Name: "24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20240418.iso", Size: 10},
		},
	}}
	s := newServer(t, st, nil)

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/noble/latest/ubuntu/ubuntu_desktop_24.04.5_amd64_20240418.iso.sha256", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("file status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://store/noble/24.04.5/ubuntu/ubuntu_desktop_24.04.5_amd64_20240418.iso.sha256" {
		t.Errorf("file Location = %q", loc)
	}

	// The latest folder is virtual: it lists the newest artifacts instead of
	// redirecting into a real version directory.
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/noble/latest/ubuntu/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dir status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "ubuntu_desktop_24.04.5_amd64_20240418.iso.sha256") {
		t.Errorf("latest directory listing missing newest artifact: %s", body)
	}

	// A codename-based virtual file follows its own target, so it redirects
	// straight to the real object.
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/noble/latest/kubuntu/kubuntu_desktop_noble_amd64.iso", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("stable file status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://store/noble/24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20240418.iso" {
		t.Errorf("stable file Location = %q", loc)
	}

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/noble/latest", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("latest status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, ">kubuntu/</a>") {
		t.Errorf("latest folder missing product directory: %s", body)
	}

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/noble/latest/kubuntu/", nil))
	if body := rec.Body.String(); !strings.Contains(body, "kubuntu_desktop_noble_amd64.iso") {
		t.Errorf("latest listing missing codename-based alias: %s", body)
	}

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/noble/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `href="/noble/latest/"`) || !strings.Contains(body, ">latest/</a>") {
		t.Errorf("listing missing latest link: %s", body)
	}
}

func TestNotFound(t *testing.T) {
	st := fakeStorage{objects: map[string][]storage.Object{
		"images": {{Name: "a.txt", Size: 10}},
	}}
	rec := httptest.NewRecorder()
	newServer(t, st, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/images/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestLegacyHostLayout checks that a host mapped to a legacy layout is served
// the legacy tree and that object requests redirect to the canonical object.
func TestLegacyHostLayout(t *testing.T) {
	st := fakeStorage{objects: map[string][]storage.Object{
		"noble": {{Name: "24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso", Size: 10}},
	}}
	layouts := map[string]vfs.Layout{"cdimage.ubuntu.com": vfs.LayoutCDImage}
	s := newServer(t, st, layouts)

	// A legacy path is translated to the canonical object.
	req := httptest.NewRequest(http.MethodGet, "/ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso", nil)
	req.Host = "cdimage.ubuntu.com"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://store/noble/24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso" {
		t.Errorf("Location = %q", loc)
	}

	// The same host serves the legacy tree root.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "cdimage.ubuntu.com"
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("root status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `href="/ubuntu/"`) {
		t.Errorf("legacy root listing missing ubuntu dir: %s", body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	st := fakeStorage{objects: map[string][]storage.Object{
		"images": {{Name: "a.txt", Size: 10}},
	}}
	rec := httptest.NewRecorder()
	newServer(t, st, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/images/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestLogRequests(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		io.WriteString(w, "hi")
	})
	req := httptest.NewRequest(http.MethodGet, "/images/a.txt?x=1", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	rec := httptest.NewRecorder()
	LogRequests(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", rec.Code)
	}
	logged := buf.String()
	for _, want := range []string{
		"method=GET",
		`uri="/images/a.txt?x=1"`,
		"host=example.com",
		`forwarded_for="203.0.113.7, 10.0.0.1"`,
		"status=418",
		"bytes=2",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log missing %q: %s", want, logged)
		}
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
