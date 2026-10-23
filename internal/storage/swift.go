package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ubuntu/ubimi/internal/config"
)

// pageSize is the number of objects requested per Swift API call.
const pageSize = 1000

// Swift is a Storage backed by a Swift-compatible object store (RadosGW),
// authenticated through Keystone v3.
type Swift struct {
	ks        *keystone
	client    *http.Client
	publicURL string
}

// New returns a Swift storage for the given configuration.
func New(cfg config.StorageConfig) *Swift {
	client := &http.Client{Timeout: 30 * time.Second}
	return &Swift{
		ks:        newKeystone(client, cfg),
		client:    client,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
	}
}

type swiftObject struct {
	Name         string `json:"name"`
	Bytes        int64  `json:"bytes"`
	ContentType  string `json:"content_type"`
	LastModified string `json:"last_modified"`
	Hash         string `json:"hash"`
}

// List implements Storage. It pages through the container listing using the
// Swift marker mechanism and calls fn for every object encountered.
func (s *Swift) List(ctx context.Context, container string, fn func(Object) error) error {
	marker := ""
	for {
		objs, err := s.page(ctx, container, marker)
		if err != nil {
			return err
		}
		for _, o := range objs {
			if err := fn(o.toObject()); err != nil {
				return err
			}
		}
		if len(objs) < pageSize {
			return nil
		}
		marker = objs[len(objs)-1].Name
	}
}

// page fetches one page of the listing, retrying once after re-authenticating
// if the token has expired.
func (s *Swift) page(ctx context.Context, container, marker string) ([]swiftObject, error) {
	for attempt := 0; ; attempt++ {
		token, endpoint, err := s.ks.credentials(ctx)
		if err != nil {
			return nil, err
		}
		u := endpoint + "/" + url.PathEscape(container) + "?format=json&limit=" + fmt.Sprint(pageSize)
		if marker != "" {
			u += "&marker=" + url.QueryEscape(marker)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Auth-Token", token)

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", container, err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			s.ks.invalidate()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("list %s: unexpected status %s", container, resp.Status)
		}
		var objs []swiftObject
		err = json.NewDecoder(resp.Body).Decode(&objs)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("list %s: decode: %w", container, err)
		}
		return objs, nil
	}
}

// URL implements Storage. It builds a public URL, escaping each path segment.
func (s *Swift) URL(container, object string) string {
	segs := strings.Split(strings.Trim(container, "/")+"/"+strings.TrimLeft(object, "/"), "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	return s.publicURL + "/" + strings.Join(segs, "/")
}

func (o swiftObject) toObject() Object {
	t, _ := time.Parse("2006-01-02T15:04:05.999999", o.LastModified)
	return Object{
		Name:         o.Name,
		Size:         o.Bytes,
		ContentType:  o.ContentType,
		LastModified: t,
		ETag:         o.Hash,
	}
}
