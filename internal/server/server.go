// Package server renders autoindex-style listings and redirects object
// requests straight to the object store.
package server

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/ubuntu/ubimi/internal/index"
	"github.com/ubuntu/ubimi/internal/storage"
)

//go:embed index.html.tmpl
var tmplFS embed.FS

var tmpl = template.Must(template.ParseFS(tmplFS, "index.html.tmpl"))

// Server serves the index for a single Storage, falling back to next when the
// index has no match (for example, the legacy redirect farm).
type Server struct {
	idx  *index.Index
	st   storage.Storage
	next http.Handler
}

// New returns a Server reading from idx, linking through st and handing
// unmatched requests to next.
func New(idx *index.Index, st storage.Storage, next http.Handler) *Server {
	return &Server{idx: idx, st: st, next: next}
}

type entry struct {
	Name     string
	URL      string
	Size     string
	Modified string
}

type pageData struct {
	Container string
	Prefix    string
	Parent    string
	Dirs      []entry
	Files     []entry
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clean := strings.Trim(r.URL.Path, "/")
	container, prefix, _ := strings.Cut(clean, "/")
	if container == "" {
		s.next.ServeHTTP(w, r)
		return
	}

	// An indexed object is served by redirecting to the object store.
	if prefix != "" {
		if _, ok := s.idx.Lookup(container, prefix); ok {
			http.Redirect(w, r, s.st.URL(container, prefix), http.StatusFound)
			return
		}
	}

	files, dirs := s.idx.Children(container, prefix)
	if len(files) == 0 && len(dirs) == 0 {
		s.next.ServeHTTP(w, r)
		return
	}

	data := pageData{Container: container, Prefix: prefix}
	if prefix != "" {
		data.Parent = "/" + container + "/" + parentOf(prefix)
	}
	base := "/" + container + "/"
	if prefix != "" {
		base += prefix + "/"
	}
	for _, d := range dirs {
		data.Dirs = append(data.Dirs, entry{Name: d, URL: base + d + "/"})
	}
	for _, f := range files {
		obj := f.Name
		data.Files = append(data.Files, entry{
			Name:     f.Name[strings.LastIndex(f.Name, "/")+1:],
			URL:      s.st.URL(container, obj),
			Size:     humanSize(f.Size),
			Modified: formatTime(f.LastModified),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
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
