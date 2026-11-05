// Package server renders autoindex-style listings and redirects object
// requests straight to the object store. Requests are mapped to the canonical
// images tree through one of the vfs layouts, selected by hostname.
package server

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
	"github.com/ubuntu/ubimi/internal/vfs"
)

//go:embed listing.html.tmpl
var tmpllistingFS embed.FS

var tmpl_listing = template.Must(template.ParseFS(tmpllistingFS, "listing.html.tmpl"))

// Server serves the canonical images tree through the layout selected for each
// request hostname, redirecting object requests to the object store.
type Server struct {
	ctx      context.Context
	idx      *index.Index
	st       storage.Storage
	layouts  map[string]vfs.Layout // lower-case hostname -> layout
	fallback vfs.Layout

	mu    sync.Mutex
	views map[vfs.Layout]*vfs.View
}

// New returns a Server serving idx and redirecting objects through st. The
// layouts map selects a tree layout per request hostname; hosts not listed fall
// back to the images layout, whose first path segment selects the container.
func New(ctx context.Context, idx *index.Index, st storage.Storage, layouts map[string]vfs.Layout) *Server {
	hosts := make(map[string]vfs.Layout, len(layouts))
	for host, layout := range layouts {
		hosts[strings.ToLower(host)] = layout
	}
	return &Server{
		ctx:      ctx,
		idx:      idx,
		st:       st,
		layouts:  hosts,
		fallback: vfs.LayoutImages,
		views:    make(map[vfs.Layout]*vfs.View),
	}
}

type entry struct {
	Name     string
	URL      string
	Size     string
	Modified string
}

type pageData struct {
	Path   string
	Parent string
	Dirs   []entry
	Files  []entry
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	view := s.view(r.Host)
	clean := strings.Trim(r.URL.Path, "/")
	name := clean
	if name == "" {
		name = "."
	}

	// "latest" is a virtual HTTP-only permalink: resolve it and redirect to
	// the concrete tree, or straight to the object store if it names an object.
	if target, ok := view.Latest(name); ok {
		if container, object, ok := view.Resolve(target); ok {
			http.Redirect(w, r, s.st.URL(container, object), http.StatusFound)
			return
		}
		http.Redirect(w, r, "/"+target, http.StatusFound)
		return
	}

	f, err := view.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if info, err := f.Stat(); err == nil && !info.IsDir() {
		// An indexed object is served by redirecting to the object store.
		container, object, ok := view.Resolve(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, s.st.URL(container, object), http.StatusFound)
		return
	}

	if err := s.renderListing(w, view, clean); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// view returns the layout view for the request host, reusing one per layout.
func (s *Server) view(host string) *vfs.View {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	layout, ok := s.layouts[strings.ToLower(name)]
	if !ok {
		layout = s.fallback
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.views[layout]
	if !ok {
		v = vfs.NewLayout(s.ctx, s.idx, s.st, layout)
		s.views[layout] = v
	}
	return v
}

func (s *Server) renderListing(w http.ResponseWriter, view *vfs.View, clean string) error {
	name := clean
	if name == "" {
		name = "."
	}
	entries, err := fs.ReadDir(view, name)
	if err != nil {
		return err
	}

	data := pageData{Path: "/"}
	if clean != "" {
		data.Path = "/" + clean + "/"
		data.Parent = "/" + parentOf(clean)
	}
	for _, e := range entries {
		full := e.Name()
		if clean != "" {
			full = clean + "/" + e.Name()
		}
		if e.IsDir() {
			data.Dirs = append(data.Dirs, entry{Name: e.Name(), URL: "/" + full + "/"})
			continue
		}
		container, object, ok := view.Resolve(full)
		if !ok {
			continue
		}
		info, _ := e.Info()
		data.Files = append(data.Files, entry{
			Name:     e.Name(),
			URL:      s.st.URL(container, object),
			Size:     humanSize(info.Size()),
			Modified: formatTime(info.ModTime()),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return tmpl_listing.Execute(w, data)
}

// parentOf returns the parent prefix with a trailing slash, or "" at the root.
func parentOf(prefix string) string {
	p := strings.TrimSuffix(prefix, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i] + "/"
	}
	return ""
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 5 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
