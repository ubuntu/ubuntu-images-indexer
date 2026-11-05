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
"storage" interface, allowing easy swapping of the Object Storage backend. The
HTTP and rsync listeners both render the canonical images tree through a
`vfs.Layout`, which projects the legacy `cdimage.ubuntu.com` and
`releases.ubuntu.com` trees out of it, so the same semantic model serves both
the old and the current layouts.

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

ubimi can optionally also speak the rsync daemon protocol
(backed by [`gokrazy/rsync`](https://github.com/gokrazy/rsync)). The object store
holds the canonical images tree as `{release}/{product}/{path}` object names,
spread across containers (one per series, named after the series codename). The
rsync listener exposes that tree through one module per legacy layout:

- `cdimage` presents the old `cdimage.ubuntu.com` tree, projecting a canonical
  object onto paths such as `ubuntu/releases/noble/release/...`;
- `releases` presents the old `releases.ubuntu.com` tree, projecting onto paths
  such as `24.04/ubuntu-...iso`;
- `images` exposes the canonical tree unchanged, but keeps each object's
  per-series container as the first path segment
  (`noble/24.04/ubuntu/...`).

The legacy layouts are projections of the parsed canonical tree rather than
string rewrites: the release series names the directory (its version, and the
container's codename wherever the legacy tree keeps both) and the object path is
carried through. The layout module (`internal/images`) discovers series,
codenames, products and point releases from the tree itself, so nothing about a
particular series is configured or hard-coded.

```sh
# in config.toml
[rsync]
listen = ":873"
modules = ["cdimage", "releases", "images"]

rsync rsync://localhost:873/cdimage/ubuntu/releases/noble/release/...
rsync rsync://localhost:873/releases/24.04/
rsync rsync://localhost:873/images/noble/24.04/ubuntu/
```

Module names other than these expose all containers directly, preserving the
old single-module behaviour. Unlike the HTTP listener — which redirects clients
to the object store — the rsync daemon has no redirect mechanism, so file
transfers are streamed from the object store on demand. The `[rsync]` section is
inert unless `listen` is set.

`gokrazy/rsync` is vendored under `vendor/` with a local patch to
`internal/sender/flist.go`: upstream does not list a non-recursively requested
directory's immediate entries, so `rsync --list-only` only emitted `.`. The
patch restores the one-level listing that stock rsync daemons produce. Note that
re-running `go mod vendor` will discard the patch.

## Configuration

See [`config.example.toml`](config.example.toml) for a full example. ubimi
authenticates against Keystone (identity API v1, v2 or v3 — inferred from
`auth_url`, or pinned with `auth_version`), discovers the `object-store` (Swift)
endpoint from the service catalog, enumerates every container in the account,
and crawls them into an in-memory index. Listing links and redirects are built from the
required `storage.public_url` so that clients always reach the object store
directly; ubimi never proxies object bytes.

Requests are served through one of the `vfs` tree layouts, selected per request
hostname by the `[layouts]` table. The `images` layout is the canonical tree
whose first path segment selects the container and whose remainder is the object
prefix, mirroring the Swift URL layout (`{public_url}/{container}/{object}`).
The `cdimage` and `releases` layouts present the legacy `cdimage.ubuntu.com` and
`releases.ubuntu.com` trees, projected from the canonical objects. Hosts not
listed fall back to the `images` layout:

```toml
[layouts]
"cdimage.ubuntu.com"  = "cdimage"
"releases.ubuntu.com" = "releases"
```

An indexed object is served with a redirect to the object store; directories are
rendered as listings. This replaces the earlier host+URI redirect farm, so the
same layout logic drives both the HTTP and rsync listeners.

### Latest permalinks

The HTTP listener also answers a synthetic `latest` segment on the canonical
layout, `{container}/latest/{product}/...`, redirecting (302) to the newest
release of that product in the series — straight to the object store when it
names an object, or to the concrete directory otherwise. For example, with
`noble` at `24.04` and its newest `ubuntu` release at `24.04.5`,
`/noble/latest/ubuntu/` redirects to `/noble/24.04.5/ubuntu/`.

The segment is entirely virtual: it is never inserted into the `fs.FS`, so it
does not appear in listings and is invisible to the rsync listener.

## Testing

```sh
go test ./...
```

## Packaging

The Go application is shipped as an OCI image ("rock") and deployed by a Juju
charm. Both are built with their respective Canonical craft tools:

```sh
rockcraft pack     # builds ubuntu-images-indexer_<version>_amd64.rock
charmcraft pack    # builds ubuntu-images-indexer_amd64.charm
```

`rockcraft pack` builds with cgo enabled, so a C toolchain is required; it is
installed inside the rock's build container.

### Deploying with Juju

```sh
juju deploy ./ubuntu-images-indexer_amd64.charm --resource ubimi-image=<image-ref>
juju config ubuntu-images-indexer \
  auth-url=https://keystone.example.com/v3 \
  user=ubimi \
  project=images \
  public-url=https://images.example.com
```

The object store password is supplied out-of-band as a Juju user secret holding
the password under the `password` key:

```sh
juju add-secret swift-password password=<secret>
juju grant-secret swift-password ubuntu-images-indexer
juju config ubuntu-images-indexer password=secret://<secret-id>
```

The charm renders the equivalent `config.toml` into the workload container at
`/etc/ubimi/config.toml` and starts ubimi under Pebble. The `ingress` relation
can expose the listings externally. The per-host `[layouts]` table is not yet
exposed through charm configuration.
