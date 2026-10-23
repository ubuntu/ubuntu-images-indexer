// Package index keeps an in-memory snapshot of the object store contents,
// refreshed by a periodic full crawl.
package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ubuntu/ubimi/internal/storage"
)

// Index holds a snapshot of every crawled container, keyed by container name
// and sorted by object name. It is safe for concurrent use.
type Index struct {
	mu   sync.RWMutex
	snap map[string][]storage.Object
}

// New returns an empty index.
func New() *Index {
	return &Index{snap: make(map[string][]storage.Object)}
}

// Refresh discovers every container, crawls the objects in each and atomically
// replaces the snapshot. Containers that fail to list retain their previous
// contents; the combined errors are returned but do not disturb the rest of
// the index.
func (i *Index) Refresh(ctx context.Context, st storage.Storage) error {
	containers, err := st.Containers(ctx)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	next := make(map[string][]storage.Object, len(containers))
	var errs []error
	for _, c := range containers {
		var objs []storage.Object
		err := st.List(ctx, c, func(o storage.Object) error {
			objs = append(objs, o)
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("container %s: %w", c, err))
			continue
		}
		slices.SortFunc(objs, func(a, b storage.Object) int { return strings.Compare(a.Name, b.Name) })
		next[c] = objs
	}

	i.mu.Lock()
	for _, c := range containers {
		if _, ok := next[c]; !ok {
			next[c] = i.snap[c]
		}
	}
	i.snap = next
	i.mu.Unlock()
	return errors.Join(errs...)
}

// Run refreshes the index every interval until ctx is cancelled.
func (i *Index) Run(ctx context.Context, st storage.Storage, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := i.Refresh(ctx, st); err != nil {
				slog.Error("index refresh", "err", err)
			}
		}
	}
}

// Children returns the objects directly under prefix and the names of the
// immediate sub-directories, both in lexicographic order. prefix is the object
// prefix within container (without a leading slash).
func (i *Index) Children(container, prefix string) (files []storage.Object, dirs []string) {
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}

	i.mu.RLock()
	defer i.mu.RUnlock()
	objs := i.snap[container]
	// The snapshot is sorted, so skip straight to the first object at or after
	// the prefix instead of scanning the whole container.
	start, _ := slices.BinarySearchFunc(objs, prefix, func(o storage.Object, target string) int {
		return strings.Compare(o.Name, target)
	})
	for _, o := range objs[start:] {
		rest, ok := strings.CutPrefix(o.Name, prefix)
		if !ok {
			break
		}
		if rest == "" {
			continue
		}
		if idx := strings.IndexByte(rest, '/'); idx >= 0 {
			if dir := rest[:idx]; len(dirs) == 0 || dirs[len(dirs)-1] != dir {
				dirs = append(dirs, dir)
			}
		} else {
			files = append(files, o)
		}
	}
	return files, dirs
}

// Lookup reports whether container/name is an indexed object.
func (i *Index) Lookup(container, name string) (storage.Object, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	objs := i.snap[container]
	idx, found := slices.BinarySearchFunc(objs, name, func(o storage.Object, target string) int {
		return strings.Compare(o.Name, target)
	})
	if !found {
		return storage.Object{}, false
	}
	return objs[idx], true
}

// Containers returns the names of every indexed container, sorted.
func (i *Index) Containers() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	names := make([]string, 0, len(i.snap))
	for c := range i.snap {
		names = append(names, c)
	}
	slices.Sort(names)
	return names
}

// HasContainer reports whether container is present in the snapshot.
func (i *Index) HasContainer(container string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	_, ok := i.snap[container]
	return ok
}
