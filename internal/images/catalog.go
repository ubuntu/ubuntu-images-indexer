package images

import (
	"cmp"
	"slices"
)

// Catalog is a semantic view of a parsed canonical tree. It discovers which
// series exist, the codenames aliased to them and the products each carries,
// and computes the latest release of every product. Everything it knows is
// read from the objects it is built from; nothing is configured.
type Catalog struct {
	series     map[Series]*seriesInfo
	aliases    map[string]Series
	containers map[string]Series
	latest     map[productKey]Version
}

type productKey struct {
	series  Series
	product string
}

type seriesInfo struct {
	codenames map[string]struct{}
	products  map[string]struct{}
}

// NewCatalog discovers the series, aliases, products and latest releases of a
// set of canonical objects. Objects without a series of their own (milestone
// releases such as "beta") contribute their product and artifacts but do not
// create a series.
func NewCatalog(objects []Object) *Catalog {
	c := &Catalog{
		series:     make(map[Series]*seriesInfo),
		aliases:    make(map[string]Series),
		containers: make(map[string]Series),
		latest:     make(map[productKey]Version),
	}

	// container -> series -> number of versioned objects, used to work out
	// which codename aliases which series.
	containers := make(map[string]map[Series]int)

	for _, o := range objects {
		s, ok := o.Series()
		if !ok {
			continue
		}
		info := c.series[s]
		if info == nil {
			info = &seriesInfo{
				codenames: make(map[string]struct{}),
				products:  make(map[string]struct{}),
			}
			c.series[s] = info
		}
		info.products[o.Product] = struct{}{}

		key := productKey{series: s, product: o.Product}
		if best, ok := c.latest[key]; !ok || o.Release.Version.Compare(best) > 0 {
			c.latest[key] = o.Release.Version
		}

		if o.Container != "" {
			if containers[o.Container] == nil {
				containers[o.Container] = make(map[Series]int)
			}
			containers[o.Container][s]++
		}
	}

	// Every series answers to its own version string.
	for s := range c.series {
		c.aliases[s.String()] = s
	}

	// A container answers to the series most of its versioned objects belong
	// to. A well-formed store keeps exactly one series per container, named
	// after the series codename; the dominant series still gives a stable
	// answer if a stray object disagrees.
	for container, bySeries := range containers {
		best, bestN := Series{}, -1
		for s, n := range bySeries {
			if n > bestN || (n == bestN && s.Compare(best) < 0) {
				best, bestN = s, n
			}
		}
		c.aliases[container] = best
		c.containers[container] = best
		c.series[best].codenames[container] = struct{}{}
	}

	return c
}

// Resolve returns the series a codename or version string names.
func (c *Catalog) Resolve(alias string) (Series, bool) {
	s, ok := c.aliases[alias]
	return s, ok
}

// ResolveContainer returns the series a container belongs to. Unlike Resolve it
// only accepts container names, not bare version strings, so it is the right
// lookup when a path segment is known to name a container.
func (c *Catalog) ResolveContainer(container string) (Series, bool) {
	s, ok := c.containers[container]
	return s, ok
}

// Series returns every discovered series, in version order.
func (c *Catalog) Series() []Series {
	out := make([]Series, 0, len(c.series))
	for s := range c.series {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Series) int { return a.Compare(b) })
	return out
}

// Codenames returns the codenames aliased to a series, sorted.
func (c *Catalog) Codenames(s Series) []string {
	info := c.series[s]
	if info == nil {
		return nil
	}
	return sortedKeys(info.codenames)
}

// Products returns the products a series carries, sorted.
func (c *Catalog) Products(s Series) []string {
	info := c.series[s]
	if info == nil {
		return nil
	}
	return sortedKeys(info.products)
}

// Latest returns the newest versioned release of a product in a series,
// reporting false when the series carries no versioned release of it.
func (c *Catalog) Latest(s Series, product string) (Version, bool) {
	v, ok := c.latest[productKey{series: s, product: product}]
	if !ok {
		return nil, false
	}
	return slices.Clone(v), true
}

// Compare orders series by year then month.
func (s Series) Compare(o Series) int {
	if n := cmp.Compare(s.Major, o.Major); n != 0 {
		return n
	}
	return cmp.Compare(s.Minor, o.Minor)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
