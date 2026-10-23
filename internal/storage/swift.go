package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	ks           *keystone
	client       *http.Client
	streamClient *http.Client
	publicURL    string
}

// New returns a Swift storage for the given configuration.
func New(cfg config.StorageConfig) *Swift {
	client := &http.Client{Timeout: 30 * time.Second}
	// streamClient has no overall timeout so that large object bodies can be
	// streamed without being cut off; only the response headers are bounded.
	streamClient := &http.Client{
		Transport: &http.Transport{ResponseHeaderTimeout: 30 * time.Second},
	}
	return &Swift{
		ks:           newKeystone(client, cfg),
		client:       client,
		streamClient: streamClient,
		publicURL:    strings.TrimRight(cfg.PublicURL, "/"),
	}
}

type swiftObject struct {
	Name         string `json:"name"`
	Bytes        int64  `json:"bytes"`
	ContentType  string `json:"content_type"`
	LastModified string `json:"last_modified"`
	Hash         string `json:"hash"`
}

// Containers implements Storage. It lists every container in the account.
func (s *Swift) Containers(ctx context.Context) ([]string, error) {
	_, endpoint, err := s.ks.credentials(ctx)
	if err != nil {
		return nil, err
	}
	var listed []struct {
		Name string `json:"name"`
	}
	if err := s.get(ctx, endpoint+"?format=json", &listed); err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	names := make([]string, len(listed))
	for i, c := range listed {
		names[i] = c.Name
	}
	return names, nil
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

// page fetches one page of a container listing.
func (s *Swift) page(ctx context.Context, container, marker string) ([]swiftObject, error) {
	_, endpoint, err := s.ks.credentials(ctx)
	if err != nil {
		return nil, err
	}
	u := endpoint + "/" + url.PathEscape(container) + "?format=json&limit=" + fmt.Sprint(pageSize)
	if marker != "" {
		u += "&marker=" + url.QueryEscape(marker)
	}
	var objs []swiftObject
	if err := s.get(ctx, u, &objs); err != nil {
		return nil, fmt.Errorf("list %s: %w", container, err)
	}
	return objs, nil
}

// get performs an authenticated GET and decodes the JSON response, retrying
// once after re-authenticating if the token has expired.
func (s *Swift) get(ctx context.Context, u string, out any) error {
	for attempt := 0; ; attempt++ {
		token, _, err := s.ks.credentials(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Auth-Token", token)

		resp, err := s.client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			s.ks.invalidate()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status %s", resp.Status)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

// URL implements Storage. It builds a public URL, escaping each path segment.
func (s *Swift) URL(container, object string) string {
	return s.publicURL + "/" + escapePath(strings.Trim(container, "/")+"/"+strings.TrimLeft(object, "/"))
}

// Open implements Storage. It streams an object's contents from offset,
// retrying once after re-authenticating if the token has expired.
func (s *Swift) Open(ctx context.Context, container, object string, offset int64) (io.ReadCloser, error) {
	for attempt := 0; ; attempt++ {
		token, endpoint, err := s.ks.credentials(ctx)
		if err != nil {
			return nil, err
		}
		u := endpoint + "/" + escapePath(container) + "/" + escapePath(object)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Auth-Token", token)
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}

		resp, err := s.streamClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("open %s/%s: %w", container, object, err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			s.ks.invalidate()
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return nil, fmt.Errorf("open %s/%s: unexpected status %s", container, object, resp.Status)
		}
		return resp.Body, nil
	}
}

// escapePath escapes each slash-separated segment of p, preserving separators.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	return strings.Join(segs, "/")
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
