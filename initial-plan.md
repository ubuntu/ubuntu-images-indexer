# Plan: `github.com/ubuntu/ubimi`

**Goal:** an in-memory index of a RadosGW/Swift object store, served as
autoindex-style HTML listings with links pointing directly at the store, plus a
host+URI redirect farm for legacy URL migration. Keystone v3 auth. Periodic full
crawl. TOML config.

**Dependencies:** `github.com/BurntSushi/toml` only. Everything else stdlib.

## Decisions (confirmed)

- Auth: Keystone v3.
- Index: periodic full crawl into memory.
- Config: TOML (`github.com/BurntSushi/toml`).
- Module path: `github.com/ubuntu/ubimi`.
- Path mapping: first URI segment = container, remainder = object prefix.
- Links: built from a required explicit `public_url` (never the Keystone
  catalog endpoint).

## 1. `go.mod`

```
module github.com/ubuntu/ubimi
go 1.22          # for slog, slices, maps, //go:embed
require github.com/BurntSushi/toml v1.x
```

## 2. `internal/config/config.go`

```go
type Config struct {
    Listen  string         `toml:"listen"`      // default ":8080"
    Storage StorageConfig  `toml:"storage"`
    Index   IndexConfig    `toml:"index"`
    Routes  []Route        `toml:"route"`
}
type StorageConfig struct {
    AuthURL, User, Password, UserDomain string
    Project, ProjectDomain, Region      string
    Containers   []string `toml:"containers"`
    PublicURL    string   `toml:"public_url"` // required; trimmed of trailing /
}
type IndexConfig struct{ RefreshInterval time.Duration `toml:"refresh_interval"` } // default 5m
type Route struct {
    Host, Path, Target string
    Status int // default 302
}
```

`Load(path)` -> decode, apply defaults, validate (`public_url`, `auth_url`,
credentials, non-empty containers). Parse durations via a small wrapper around
`toml.UnmarshalText` or accept a string field and parse.

## 3. `internal/storage/`

```go
type Object struct {
    Name         string
    Size         int64
    ContentType  string
    LastModified time.Time
    ETag         string
}
type Storage interface {
    List(ctx context.Context, container string, fn func(Object) error) error
    URL(container, object string) string // publicURL + "/" + container + "/" + object
}
```

**`keystone.go`** - token source:

- POST `{AuthURL}/auth/tokens` body:
  `{"auth":{"identity":{"methods":["password"],"password":{"user":{"name":User,"domain":{"name":UserDomain},"password":Password}}},"scope":{"project":{"name":Project,"domain":{"name":ProjectDomain}}}}}`
- Read `X-Subject-Token` + parse token body for catalog; pick `object-store`
  service, interface `public` (or `Region` if set) -> storage endpoint.
- Struct with `Token() (string, error)`, expiry from `expires_at`,
  mutex-guarded; refresh when within ~1 min of expiry or on `401`.
- Note: catalog endpoint is **only** used for API calls; links always use
  configured `PublicURL`.

**`swift.go`** - `List`:

- Loop `GET {storageEndpoint}/{container}?format=json&limit=1000&marker={last}`
  with `X-Auth-Token`.
- Decode `[]Object` (fields `name, bytes, content_type, last_modified, hash`),
  call `fn` per object, stop when page < limit or empty.
- On `401`, force token refresh and retry once.

## 4. `internal/index/index.go`

```go
type Index struct { mu sync.RWMutex; snap map[string][]storage.Object } // container -> sorted objects
func (i *Index) Refresh(ctx, st storage.Storage) error   // crawl all configured containers, swap snapshot atomically
func (i *Index) Children(container, prefix string) (files []storage.Object, dirs []string)
```

- `Children`: binary-search the sorted slice to the prefix range; immediate
  children dedup into dirs by next `/`; exact non-dir names become files.
- `Run(ctx, st, interval)` ticker loop.

## 5. `internal/router/router.go`

```go
func Handler(rules []config.Route, next http.Handler) http.Handler
```

- Match `Host` (empty = any, `*.suffix` = suffix match, else exact incl.
  optional port), then `Path` prefix; first match wins.
- Build `Target` by replacing `{path}` with the URI remainder and
  `{container}` with first path segment; `http.Redirect`.
- Fall through to `next` when nothing matches.

## 6. `internal/server/server.go`

- Handler resolves request path -> `container = first segment`, `prefix = rest`.
- If exact object exists in index -> `302` to `st.URL(container, prefix)`.
- Else if `Children` non-empty -> render listing: dirs link to
  `/{container}/{dir}/`, files link to `st.URL(...)` with size/date columns.
  `html/template` embedded via `//go:embed`; minimal inline CSS.
- Else `404`.
- `GET`/`HEAD` only.

## 7. `cmd/ubimi/main.go`

- `-config` flag (default `config.toml`).
- Build storage (Keystone + Swift), router wrapping server, `http.Server` with
  sane timeouts.
- Start index in a goroutine; `signal.NotifyContext` for SIGINT/SIGTERM ->
  `Shutdown`.
- `log/slog` structured logging.

## 8. Tests & docs

- `router_test.go`: host/path matching, template substitution.
- `index_test.go`: `Children` with nested prefixes, dir vs file, sorted order.
- `server_test.go`: httptest with a fake `Storage` - object redirect, listing, 404.
- `config_test.go`: defaults + validation errors.
- `config.example.toml` and a README "Building / Running / Configuration" section.

## 9. Milestones (commit-sized)

1. `go.mod`, `config` + example.
2. `storage` interface + fake.
3. `keystone.go`.
4. `swift.go`.
5. `index`.
6. `server` + template.
7. `router`.
8. `main` + shutdown.
9. tests + README.

## Environment note

No Go toolchain is installed in the container (`go` not found). Install with
`sudo apt install golang-go` (or a newer Go from upstream) before building;
`gofmt` / `go vet` / `go test` can then be run.
