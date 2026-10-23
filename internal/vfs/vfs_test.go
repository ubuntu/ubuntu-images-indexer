package vfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

type fakeStorage struct {
	objects map[string][]storage.Object
	content map[string]string
}

func (f fakeStorage) Containers(context.Context) ([]string, error) {
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

func (f fakeStorage) Open(_ context.Context, container, object string, offset int64) (io.ReadCloser, error) {
	c, ok := f.content[container+"/"+object]
	if !ok {
		return nil, errors.New("no such object")
	}
	if offset > 0 {
		c = c[offset:]
	}
	return io.NopCloser(strings.NewReader(c)), nil
}

func (f fakeStorage) URL(container, object string) string {
	return "https://store/" + container + "/" + object
}

func newFS(t *testing.T) *FS {
	t.Helper()
	st := fakeStorage{
		objects: map[string][]storage.Object{
			"images": {
				{Name: "a.txt", Size: 5},
				{Name: "dir/b.txt", Size: 6},
			},
			"other": {},
		},
		content: map[string]string{
			"images/a.txt":     "hello",
			"images/dir/b.txt": "world!",
		},
	}
	idx := index.New()
	if err := idx.Refresh(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return New(context.Background(), idx, st)
}

func TestOpenRoot(t *testing.T) {
	f := newFS(t)
	entries, err := fs.ReadDir(f, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 || entries[0].Name() != "images" || !entries[0].IsDir() {
		t.Fatalf("root entries = %v", entries)
	}
}

func TestOpenContainerAndDir(t *testing.T) {
	f := newFS(t)
	entries, err := fs.ReadDir(f, "images")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want a.txt and dir", entries)
	}
	if entries[0].Name() != "a.txt" || entries[0].IsDir() {
		t.Errorf("first entry = %+v", entries[0])
	}
	if entries[1].Name() != "dir" || !entries[1].IsDir() {
		t.Errorf("second entry = %+v", entries[1])
	}

	entries, err = fs.ReadDir(f, "images/dir")
	if err != nil {
		t.Fatalf("ReadDir dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "b.txt" {
		t.Fatalf("dir entries = %v", entries)
	}
}

func TestReadFileAndStat(t *testing.T) {
	f := newFS(t)
	file, err := f.Open("images/dir/b.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Name() != "b.txt" || info.Size() != 6 || info.IsDir() {
		t.Errorf("info = %+v", info)
	}

	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "world!" {
		t.Errorf("content = %q", got)
	}
}

func TestSeek(t *testing.T) {
	f := newFS(t)
	file, err := f.Open("images/a.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()

	if _, err := file.(io.Seeker).Seek(1, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "ello" {
		t.Errorf("after seek content = %q, want %q", got, "ello")
	}

	// SeekEnd reports the size.
	pos, err := file.(io.Seeker).Seek(0, io.SeekEnd)
	if err != nil || pos != 5 {
		t.Errorf("SeekEnd = %d, %v; want 5", pos, err)
	}
}

func TestNotExist(t *testing.T) {
	f := newFS(t)
	if _, err := f.Open("images/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open missing = %v, want ErrNotExist", err)
	}
	if _, err := f.Open("../etc/passwd"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open invalid = %v, want ErrNotExist", err)
	}
}

func TestWalk(t *testing.T) {
	f := newFS(t)
	var got []string
	err := fs.WalkDir(f, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		got = append(got, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	want := []string{".", "images", "images/a.txt", "images/dir", "images/dir/b.txt", "other"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("walk = %v, want %v", got, want)
	}
}
