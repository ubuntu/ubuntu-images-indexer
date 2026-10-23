package index

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/ubuntu/ubimi/internal/storage"
)

type fakeStorage struct {
	objects map[string][]storage.Object
	listErr map[string]error // per-container List failures
}

func (f fakeStorage) Containers(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(f.objects))
	for name := range f.objects {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func (f fakeStorage) List(_ context.Context, container string, fn func(storage.Object) error) error {
	if err := f.listErr[container]; err != nil {
		return err
	}
	for _, o := range f.objects[container] {
		if err := fn(o); err != nil {
			return err
		}
	}
	return nil
}

func (f fakeStorage) Open(context.Context, string, string, int64) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (f fakeStorage) URL(container, object string) string {
	return "https://store/" + container + "/" + object
}

func obj(name string) storage.Object { return storage.Object{Name: name} }

func newStore() fakeStorage {
	return fakeStorage{objects: map[string][]storage.Object{
		"images": {obj("z.txt"), obj("dir/d.txt"), obj("a.txt"), obj("dir/sub/c.txt"), obj("dir/b.txt")},
	}}
}

func TestRefreshSortsAndChildren(t *testing.T) {
	idx := New()
	if err := idx.Refresh(context.Background(), newStore()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	files, dirs := idx.Children("images", "")
	if got := names(files); len(got) != 2 || got[0] != "a.txt" || got[1] != "z.txt" {
		t.Errorf("root files = %v, want [a.txt z.txt]", got)
	}
	if len(dirs) != 1 || dirs[0] != "dir" {
		t.Errorf("root dirs = %v, want [dir]", dirs)
	}

	files, dirs = idx.Children("images", "dir")
	if got := names(files); len(got) != 2 || got[0] != "dir/b.txt" || got[1] != "dir/d.txt" {
		t.Errorf("dir files = %v, want [dir/b.txt dir/d.txt]", got)
	}
	if len(dirs) != 1 || dirs[0] != "sub" {
		t.Errorf("dir dirs = %v, want [sub]", dirs)
	}

	files, dirs = idx.Children("images", "dir/sub")
	if len(files) != 1 || len(dirs) != 0 {
		t.Errorf("sub = %v/%v, want one file, no dirs", names(files), dirs)
	}
}

func TestLookup(t *testing.T) {
	idx := New()
	if err := idx.Refresh(context.Background(), newStore()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, ok := idx.Lookup("images", "dir/b.txt"); !ok {
		t.Error("Lookup existing object = false")
	}
	if _, ok := idx.Lookup("images", "nope"); ok {
		t.Error("Lookup missing object = true")
	}
}

func TestRefreshRetainsOnError(t *testing.T) {
	idx := New()
	if err := idx.Refresh(context.Background(), newStore()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	broken := fakeStorage{
		objects: newStore().objects,
		listErr: map[string]error{"images": errors.New("boom")},
	}
	if err := idx.Refresh(context.Background(), broken); err == nil {
		t.Fatal("Refresh = nil, want error")
	}
	if _, ok := idx.Lookup("images", "a.txt"); !ok {
		t.Error("previous snapshot was not retained after a failed container listing")
	}
}

func TestRefreshDropsRemovedContainers(t *testing.T) {
	idx := New()
	if err := idx.Refresh(context.Background(), newStore()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if err := idx.Refresh(context.Background(), fakeStorage{objects: map[string][]storage.Object{}}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, ok := idx.Lookup("images", "a.txt"); ok {
		t.Error("container that no longer exists is still indexed")
	}
}

func names(objs []storage.Object) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.Name
	}
	return out
}
