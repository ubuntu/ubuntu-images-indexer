package vfs

import (
	"sync"

	"github.com/ubuntu/ubimi/internal/images"
	"github.com/ubuntu/ubimi/internal/index"
)

// legacyContainers hold the old tree layouts (cdimage.ubuntu.com and
// releases.ubuntu.com). Their objects are not part of the canonical images
// tree and must not be translated into it.
var legacyContainers = map[string]bool{
	string(LayoutCDImage):  true,
	string(LayoutReleases): true,
}

// Catalog is the semantic model of the canonical images tree discovered from
// the index: the series, codenames, products and latest releases. Parsing
// every indexed object is layout-independent work, so one Catalog is shared
// by every View and recomputed only when the index snapshot changes.
type Catalog struct {
	idx *index.Index

	mu      sync.Mutex
	version uint64
	loaded  bool
	catalog *images.Catalog
}

// NewCatalog returns the semantic model of the tree held by idx.
func NewCatalog(idx *index.Index) *Catalog {
	return &Catalog{idx: idx}
}

// Load returns the catalog of the current index snapshot, re-parsing the
// snapshot when its version changed since the last call.
func (c *Catalog) Load() *images.Catalog {
	c.mu.Lock()
	defer c.mu.Unlock()

	version := c.idx.Version()
	if c.loaded && c.version == version {
		return c.catalog
	}

	var parsed []images.Object
	for _, container := range c.idx.Containers() {
		if legacyContainers[container] {
			continue
		}
		for _, obj := range c.idx.Objects(container) {
			if o, ok := images.Parse(container, obj.Name); ok {
				parsed = append(parsed, o)
			}
		}
	}
	c.catalog = images.NewCatalog(parsed)
	c.version = version
	c.loaded = true
	return c.catalog
}
