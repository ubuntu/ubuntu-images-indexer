// Package storage abstracts the object storage backend behind a small
// interface so the Swift/RadosGW implementation can be swapped out.
package storage

import (
	"context"
	"time"
)

// Object is a single stored object as reported by the backend listing.
type Object struct {
	Name         string
	Size         int64
	ContentType  string
	LastModified time.Time
	ETag         string
}

// Storage is the read-only view of an object store that ubimi needs.
type Storage interface {
	// Containers returns the names of every container in the account.
	Containers(ctx context.Context) ([]string, error)
	// List calls fn for every object in container, in lexicographic order.
	List(ctx context.Context, container string, fn func(Object) error) error
	// URL returns the public URL a client should be sent to for an object.
	URL(container, object string) string
}
