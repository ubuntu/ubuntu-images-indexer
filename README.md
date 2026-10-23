# ubimi - Ubuntu Image Indexer

This is a simple `go` program that keeps and serves an in-memory index of a
RadosGW provided swift storage. This is essentially a replacement for Apache's
autoindex feature, against a more modern Object Storage.

## Architecture

Over HTTP, this program is not intended to serve the bytes stored on that
object storage, but rather simply points to them. An optional rsync listener,
served on a separate port, instead streams the object bytes for mirroring.
Intended deployement is as follow:
```
                  http clients
                       |
                       V
        +----------> HAProxy ------------------+
        |                                      |
frontend filtering /images/              default frontend
   serves bytes (http)                         |
        |                                      |
        V      +<----- indexes --------<+      V
    RadosGW <--|                        |--> ubimi <---- rsync clients
               +>-serves bytes (rsync)->+               (rsync://:873)
```

ubimi indexes RadosGW over the Swift API; HTTP clients are then redirected
straight to RadosGW, while rsync clients (on a separate listener) are served
the object bytes through ubimi.

The architecture of ubimi keeps the swift calls abstracted behind a generic
"storage" interface, allowing easy swapping of the Object Storage backend.
Similarly, a separate routing interface allows easy addition of any HTTP routes
based on both the hostname and the URI to provide an easy to maintain redirect
farm, facilitating the transition from previous legacy storage solution.

## Local development

For local development, the [`saio`](https://hub.docker.com/r/openstackswift/saio)
Docker images can provide a dead simple throw-away `swift` interface.

## Building and running

```sh
go build ./cmd/ubimi
./ubimi -config config.toml
```

Building requires a C toolchain (cgo), because the rsync support pulls in the
`gokrazy/rsync` dependency chain (see below).

## rsync listener

ubimi can optionally also speak the rsync daemon protocol, exposing the index
as a single read-only module whose top-level directories are the containers
(backed by [`gokrazy/rsync`](https://github.com/gokrazy/rsync)):

```sh
# in config.toml
[rsync]
listen = ":873"
module = "ubimi"

rsync rsync://localhost:873/ubimi/images/...
```

Unlike the HTTP listener — which redirects clients to the object store — the
rsync daemon has no redirect mechanism, so file transfers are streamed from the
object store on demand. The `[rsync]` section is inert unless `listen` is set.

## Configuration

See [`config.example.toml`](config.example.toml) for a full example. ubimi
authenticates against Keystone v3, discovers the `object-store` (Swift) endpoint
from the service catalog, enumerates every container in the account, and crawls
them into an in-memory index. Listing links and redirects are built from the
required `storage.public_url` so that clients always reach the object store
directly; ubimi never proxies object bytes.

The first path segment of a request selects the container and the remainder is
the object prefix, mirroring the Swift URL layout
(`{public_url}/{container}/{object}`). The in-memory index is consulted first;
only when it has no matching object or listing are the ordered `[[route]]` rules
tried. Those rules match on host and either a path prefix or a regular
expression, allowing legacy URLs to be migrated to the current layout:

- a `path` prefix rule may reference `{path}` (the URI remainder) and
  `{container}` (the first path segment) in its target;
- a `match` rule is a regular expression whose named capture groups are
  referenced as `{name}` in the target, so segments can be reordered, e.g.
  `^/(?P<product>[^/]+)/releases/(?P<series>[^/]+)/?$` →
  `https://images.ubuntu.com/images/{series}/{product}`.

## Testing

```sh
go test ./...
```
