package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ncw/swift/v2"

	"github.com/ubuntu/ubimi/internal/config"
)

// Swift is a Storage backed by a Swift-compatible object store (RadosGW).
// Authentication (Keystone v1, v2 or v3), token caching, catalog discovery and
// retrying on an expired token are all handled by the ncw/swift client.
type Swift struct {
	conn      *swift.Connection
	publicURL string
}

// New returns a Swift storage for the given configuration.
func New(cfg config.StorageConfig) *Swift {
	return &Swift{
		conn: &swift.Connection{
			UserName:     cfg.User,
			ApiKey:       cfg.Password,
			AuthUrl:      cfg.AuthURL,
			Domain:       cfg.UserDomain,
			Tenant:       cfg.Project,
			TenantDomain: cfg.ProjectDomain,
			Region:       cfg.Region,
			AuthVersion:  cfg.AuthVersion,
		},
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
	}
}

// Containers implements Storage. It lists every container in the account.
func (s *Swift) Containers(ctx context.Context) ([]string, error) {
	names, err := s.conn.ContainerNamesAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	return names, nil
}

// List implements Storage. It pages through the container listing and calls fn
// for every object encountered.
func (s *Swift) List(ctx context.Context, container string, fn func(Object) error) error {
	var callbackErr error
	err := s.conn.ObjectsWalk(ctx, container, nil, func(ctx context.Context, opts *swift.ObjectsOpts) (any, error) {
		objs, err := s.conn.Objects(ctx, container, opts)
		if err != nil {
			return nil, err
		}
		for _, o := range objs {
			if err := fn(toObject(o)); err != nil {
				callbackErr = err
				return nil, err
			}
		}
		return objs, nil
	})
	if callbackErr != nil {
		return callbackErr
	}
	if err != nil {
		return fmt.Errorf("list %s: %w", container, err)
	}
	return nil
}

// URL implements Storage. It builds a public URL, escaping each path segment.
func (s *Swift) URL(container, object string) string {
	return s.publicURL + "/" + escapePath(strings.Trim(container, "/")+"/"+strings.TrimLeft(object, "/"))
}

// Open implements Storage. It streams an object's contents from offset.
func (s *Swift) Open(ctx context.Context, container, object string, offset int64) (io.ReadCloser, error) {
	var headers swift.Headers
	if offset > 0 {
		headers = swift.Headers{"Range": fmt.Sprintf("bytes=%d-", offset)}
	}
	file, _, err := s.conn.ObjectOpen(ctx, container, object, false, headers)
	if err != nil {
		return nil, fmt.Errorf("open %s/%s: %w", container, object, err)
	}
	return file, nil
}

// escapePath escapes each slash-separated segment of p, preserving separators.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	return strings.Join(segs, "/")
}

// toObject converts a swift.Object into the storage.Object the index uses.
func toObject(o swift.Object) Object {
	return Object{
		Name:         o.Name,
		Size:         o.Bytes,
		ContentType:  o.ContentType,
		LastModified: o.LastModified,
		ETag:         o.Hash,
	}
}
