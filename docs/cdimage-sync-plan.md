# Plan: `cmd/cdimage-sync`

## Goal

Add a new binary, `cmd/cdimage-sync`, that:

1. inventories the released `cdimage.ubuntu.com` tree with `rsync`;
2. downloads changed/new artifacts over HTTP;
3. translates each cdimage path into the canonical images layout;
4. obtains the serial for ISOs from the ISO's `.disk/info` via `bsdtar`;
5. falls back to the source file's mtime for non-ISO images;
6. uploads the canonical objects with `swift upload`;
7. never deletes and never updates an existing canonical object.

The binary is a one-shot, idempotent, add-only sync. It is not a daemon and it does
not replace the ubimi HTTP/rsync listeners. It only writes the canonical tree that
ubimi already indexes and serves.

## Why the existing layout/vfs matters

The current `internal/vfs` projection is one-way: canonical Swift objects to the
legacy cdimage/releases trees. It is deliberately lossy in ways that matter here:

- the cdimage filename does not carry the serial;
- one canonical object can project to several cdimage paths (version and codename
  directories, core `current` and dated directories);
- folder-level `SHA256SUMS` and `SHA256SUMS.gpg` are synthetic aliases, not
  canonical objects;
- the server/WSL canonical products are special-cased;
- the netboot branches model expanded trees, which this sync will not create.

Therefore the sync must not invert `vfs.View` as a filesystem. It needs an explicit
reverse translator. The vfs remains the **contract** for the cdimage path grammar
and the **verification oracle** after upload.

Reuse from the existing code:

- `internal/images` for the canonical artifact grammar and semantic model;
- `internal/vfs` forward rules (`legacyName`, `releaseName`, `dailyName`,
  `coreName`, `repeatedSubproject`, `releaseDirs`, `releaseDir`, `aliases`);
- `internal/vfs.Catalog` for series/codename/container mapping;
- `internal/index` for the in-memory Swift snapshot;
- `internal/storage` for read-only Swift access;
- `internal/vfs.View` for end-to-end verification, including synthetic checksum
  aliases and the `latest` overlay.

## Source tree and filtering

### Include

Only released artifacts are mirrored. The source tree is walked from the rsync
listing and filtered to:

- `<product>/releases/<version-or-codename>/release/...`
  - desktop, flavours, base, mini-iso, etc.;
  - the released server/WSL artifacts under `ubuntu/releases/...`;
- `ubuntu-core/<major>/stable/...` released core images;
- netboot **tarballs** under `<product>/releases/<series>/release/...`.

### Exclude

- all daily builds:
  - `ubuntu-server/.../daily-live/...`
  - `ubuntu-server/.../daily-preinstalled/...`
  - `ubuntu-wsl/.../daily-*`;
- all expanded netboot directories:
  - top-level `netboot/...`
  - `<product>/releases/<series>/release/netboot/...`
- expanded core `current`/dated aliases as separate uploads; the canonical target
  is the released core object, and the vfs `aliases` mechanism resolves the
  cdimage path;
- `SHA256SUMS` and `SHA256SUMS.gpg` as direct uploads; they are used as checksum
  sources and duplicated per artifact.

### Deduplicate version and codename directories

The source may expose the same release under both its version and its codename:

```text
ubuntu/releases/24.04/release/...
ubuntu/releases/noble/release/...
```

Both map to the same canonical object. The sync canonicalises both to one target
and relies on the index/HEAD gate to avoid a second upload.

## Canonical mapping

### General release artifact

Source:

```text
<product>/releases/<series-or-codename>/release/<legacy-name>
```

Canonical:

```text
<container>/<release>/<product>/<canonical-name>
```

where:

- `container` is the series codename from the source path or the catalog;
- `release` is the point-release version from the source filename;
- `product` is the source product, except for the server/WSL special case;
- `canonical-name` is the underscore-separated artifact name.

Examples:

```text
ubuntu/releases/24.04/release/ubuntu-24.04.5-desktop-amd64.iso
→ noble/24.04.5/ubuntu/ubuntu_desktop_24.04.5_amd64_20260423-1.iso

kubuntu/releases/24.04/release/kubuntu-24.04.5-desktop-amd64.iso
→ noble/24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20260423-1.iso

ubuntu-base/releases/24.04/release/ubuntu-base-24.04.5-base-amd64.tar.gz
→ noble/24.04.5/ubuntu-base/ubuntu-base_24.04.5_amd64_20260423-1.tar.gz

ubuntu/releases/24.04/release/ubuntu-24.04.5-netboot-arm64+largemem.tar.gz
→ noble/24.04.5/ubuntu/ubuntu_netboot_24.04.5_arm64+largemem_20260423-1.tar.gz
```

The last example is a netboot tarball. It is stored as an ordinary artifact. It is
never expanded.

### Server/WSL special case

Canonical products are:

- `ubuntu-server`
- `ubuntu-wsl`

Source released artifacts live under the `ubuntu` product in the source tree:

```text
ubuntu/releases/<series>/release/ubuntu-<version>-live-server-<arch>.iso
ubuntu/releases/<series>/release/ubuntu-<version>-preinstalled-server-<arch>.img.xz
ubuntu/releases/<series>/release/ubuntu-<version>-wsl-<arch>.wsl
```

Canonical mapping:

```text
ubuntu/releases/26.04/release/ubuntu-26.04-live-server-amd64.iso
→ resolute/26.04/ubuntu-server/ubuntu-server_live_26.04_amd64_20260418.iso

ubuntu/releases/26.04/release/ubuntu-26.04-preinstalled-server-amd64.img.xz
→ resolute/26.04/ubuntu-server/ubuntu-server_preinstalled_26.04_amd64_20260418.img.xz

ubuntu/releases/26.04/release/ubuntu-26.04-wsl-amd64.wsl
→ resolute/26.04/ubuntu-wsl/ubuntu-wsl_26.04_amd64_20260418.wsl
```

The canonical artifact `Project` is `ubuntu-server`/`ubuntu-wsl`; the source
filename's project is `ubuntu`; the server subproject is `live`/`preinstalled`;
the WSL subproject is empty.

The vfs must special-case these products in the forward direction:

```text
ubuntu-server/... → ubuntu/releases/<series>/release/ubuntu-<version>-<subproject>-server-<arch><ext>
ubuntu-wsl/...    → ubuntu/releases/<series>/release/ubuntu-<version>-wsl-<arch><ext>
```

The daily `ubuntu-server`/`ubuntu-wsl` branches in `LayoutCDImage` are not used by
this sync. They can remain for old data or be removed when daily builds are
dropped.

### Core stable

Source:

```text
ubuntu-core/<major>/stable/<dated-or-current>/<name>
```

Canonical:

```text
<container>/<series>/ubuntu-core/<canonical-name>
```

Example:

```text
ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.img.xz
→ noble/24.04/ubuntu-core/ubuntu_core_24_amd64_20261015-1.img.xz
```

The canonical release is the series (`24.04`), not the artifact version (`24`).
The canonical artifact's `Project` is `ubuntu`, `Subproject` is `core`, and the
`Version` field is `24`. The vfs `coreName` builder reconstructs the source name.

### Netboot tarballs

Only tarballs are stored. Expanded netboot folders are ignored.

Source:

```text
<product>/releases/<series>/release/<legacy-name>-netboot-<arch>.tar.gz
```

Canonical:

```text
<container>/<release>/<product>/<canonical-name>.tar.gz
```

The tarball is a normal artifact with subproject `netboot`. It is not expanded,
not unpacked, and not turned into a `netboot` product object. The vfs's expanded
netboot branches are not exercised by this sync.

### Checksums

Source release directory contains:

```text
SHA256SUMS
SHA256SUMS.gpg
```

They are not uploaded as canonical folder-level files. Instead, for every
canonical artifact in that source release directory:

```text
<canonical-name>.sha256      = bytes of SHA256SUMS
<canonical-name>.sha256.gpg  = bytes of SHA256SUMS.gpg
```

The vfs `addChecksumAliases` then synthesises the folder-level `SHA256SUMS` and
`SHA256SUMS.gpg` for the cdimage layout. This is exactly the existing mechanism
and requires no new canonical file type.

The checksum duplication includes netboot tarballs and server/WSL artifacts. The
source `SHA256SUMS` is parsed only for validation; it is never split or rewritten.

### Companion files

Regular files in the release directory (`.manifest`, `.list`, `.zsync`,
`.torrent`, etc.) are ordinary artifacts. They reuse the serial of the artifact
they accompany. Their canonical name is the canonical artifact name plus the
original extension.

Example:

```text
ubuntu-24.04.5-desktop-amd64.iso.manifest
→ ubuntu_desktop_24.04.5_amd64_<serial>.iso.manifest
```

## Serial rules

### Canonical serial type

Add a serial parser to `internal/images`:

```go
type Serial struct {
    Date  time.Time // or YYYYMMDD string
    Build int       // 0 means no suffix
}
```

Canonical spelling:

```text
YYYYMMDD
YYYYMMDD-X
```

Legacy/source spelling:

```text
YYYYMMDD
YYYYMMDD.X
```

The source `.disk/info` and mtime fallback use the dot form; the canonical object
uses the hyphen form. The parser accepts both and `String()` emits the canonical
form.

### ISO serials

For every `.iso` source file:

```sh
bsdtar -x -f <iso_path> -O ./.disk/info
```

Parse a line in the form:

```text
Ubuntu 26.04 "Resolute Raccoon" Release arm64 (20260423.1)
```

Extract:

- version/series: `26.04`
- codename display: `Resolute Raccoon`
- architecture: `arm64`
- serial: `20260423.1`

Validate against the source path/filename:

- `.disk/info` series matches the source path series;
- `.disk/info` architecture matches the source filename architecture;
- `.disk/info` codename maps to the source path codename/container.

The canonical serial is `20260423-1`.

### Non-ISO serials

For non-ISO images (`.img.xz`, `.wsl`, `.tar.gz`, etc.), use the source file's
mtime date as the serial date. If multiple builds of the same artifact identity
occur on the same day, sort by mtime ascending and assign the first build no
suffix, then `.1`, `.2`, etc. The canonical serial replaces the dot with a
hyphen.

Companion files reuse the serial of the artifact they accompany. Checksum
duplicates reuse the artifact's serial.

### Parser changes

`internal/images.ParseArtifact` currently expects exactly 8 digits in the date
field and splits the extension at the first dot. It must accept:

```text
YYYYMMDD
YYYYMMDD-X
```

and keep the extension split correct. The canonical artifact name is still
underscore-separated; the serial's `-X` is inside the date field.

`validArchitecture` currently accepts only base names. It must accept the source
variants used by cdimage, such as:

```text
arm64+largemem
arm64+raspi
riscv64+icicle
riscv64+jh7110
riscv64+licheerv
riscv64+nezha
riscv64+pic64gx
riscv64+unmatched
```

The validation rule should be: accept a known base architecture optionally followed
by `+<suffix>`. The suffix is preserved in the canonical artifact name and in the
legacy filename.

## Reverse translator

Add a translator, preferably a new `internal/synchronizer/cdimage` package
that imports the vfs helpers. It must not be a method on `View`; it is a pure
translation from source path + metadata to canonical targets.

Suggested types:

```go
type SourceFile struct {
    Path    string      // path relative to the cdimage root
    Size    int64
    MTime   time.Time
    IsDir   bool
    IsLink  bool
    LinkTo  string
}

type SourceMeta struct {
    Serial   images.Serial // from .disk/info or mtime
    Product  string        // canonical product
    Release  string        // canonical release
    Kind     TargetKind    // artifact, checksum, companion
}

type Target struct {
    Container string
    Object    string
    Source    string
    Kind      TargetKind
    Serial    images.Serial
    Product   string
    Release   string
}
```

The translator should:

1. split the source path into product / releases / series-or-codename / release /
   filename;
2. reject daily and expanded netboot paths;
3. parse the legacy filename into project/subproject/version/arch/extension;
4. apply the product mapping, including server/WSL and repeated subprojects;
5. select the canonical container from the source codename or the catalog;
6. select the canonical release from the artifact version, except core where it is
   the series;
7. attach the serial from the metadata;
8. build the canonical object name;
9. return one or more targets for checksum duplication.

The translator must be tested against the forward projection: for every canonical
target, `LayoutCDImage.virtual(container, object)` must contain the source path
(or one of its canonical aliases). This is the round-trip guard.

## VFS changes

### 1. Server/WSL released projection

Change `LayoutCDImage.virtual` for canonical products `ubuntu-server` and
`ubuntu-wsl` so released artifacts project to:

```text
ubuntu/releases/<dir>/release/<released-name>
```

where `<dir>` is the version and/or codename, and `<released-name>` is:

```text
ubuntu-<version>-<subproject>-server-<arch><ext>  // server
ubuntu-<version>-wsl-<arch><ext>                 // wsl
```

The existing daily branches are not used by the sync. Keep them only if old daily
data must still be served.

### 2. Serial-aware newest selection

The vfs currently chooses a projection when multiple canonical objects map to the
same cdimage path. With add-only multiple builds per point release, it must compare
release version and then serial, newest wins. This affects:

- `View.ensureLocked`: choose the newest serial when a shared path is projected;
- `addChecksumAliases`: choose the newest serial's checksum for the folder-level
  `SHA256SUMS`/`SHA256SUMS.gpg`;
- `latestOverlay`: choose the newest serial for each product in the `latest`
  folder and its stable aliases.

The release-level `latest` symlink can still point at the release version; the
artifact selection inside that release must be serial-aware.

### 3. Tests

Extend `internal/vfs/layout_test.go` and add translation tests covering:

- desktop/flavour/base/mini-iso release mapping;
- server live/preinstalled and WSL released mapping;
- core stable mapping and dated/current aliases;
- netboot tarball as an ordinary artifact;
- checksum duplication and synthetic folder-level aliases;
- version and codename source directories mapping to one target;
- `+` architecture variants;
- multiple serials of the same release, with newest-wins behaviour;
- `latest` and checksum alias selection across serials;
- round-trip canonical → virtual → canonical.

## Storage changes

`internal/storage.Storage` remains read-only. Add a read-only HEAD/stat method:

```go
Stat(ctx context.Context, container, object string) (Object, bool, error)
```

The Swift implementation uses the object HEAD endpoint. The sync uses it as the
authoritative add-only gate, because the index can be stale and the vfs `View` is
a projection rather than an identity check.

Do not add write methods to `storage.Storage`. The sync shells out to
`swift upload` as requested. Keeping the write path outside the read-only storage
interface preserves ubimi's design.

## Configuration

Extend `internal/config` with a `[cdimage_sync]` section. Unknown keys remain
rejected. Reuse the existing `[storage]` credentials for Swift and the index.

Suggested fields:

```toml
[cdimage_sync]
rsync_url = "rsync://cdimage.ubuntu.com/cdimage"
http_url  = "https://cdimage.ubuntu.com"
work_dir  = "/var/tmp/cdimage-sync"
concurrency = 4
dry_run = false
verify = true
bsdtar = "bsdtar"
swift = "swift"
rsync = "rsync"
```

The exact rsync module name must be confirmed against the source. The HTTP base URL
is used for downloads and should match the rsync tree.

Optional filters:

```toml
[cdimage_sync]
include = ["*/releases/*/release/*", "ubuntu-core/*/stable/*/*"]
exclude = ["*/daily-*", "*/netboot/*", "netboot/*"]
```

Defaults should implement the released-only and no-expanded-netboot rules.

## CLI

`cmd/cdimage-sync/main.go`:

- `-config` path, default `config.toml`;
- `-dry-run` prints the target plan without downloading or uploading;
- `-json` emits a machine-readable plan/summary;
- `-verify` resolves every source path through the vfs after upload;
- `-limit`/`-filter` for debugging;
- `-once` is the default; no daemon mode.

Exit status:

- `0` all planned targets already exist or were uploaded and verified;
- `1` one or more targets failed;
- `2` configuration/usage error.

Logging uses `log/slog`, matching the existing ubimi code.

## Pipeline

### 1. Build the current index

- Load config.
- Build Swift storage from the existing `[storage]` section.
- `index.Refresh(ctx, storage)`.
- `vfs.NewCatalog(idx)`.
- `vfs.View(ctx, storage, LayoutImages)` and `LayoutCDImage`.

### 2. Inventory the source

Run `rsync --list-only` (or the configured rsync command) against the source. Parse
the long listing into `SourceFile` records:

- path, size, mtime, directory/symlink, link target;
- skip directories and symlinks except where a symlink is needed to resolve a
  core `current`/dated alias;
- skip daily and expanded netboot paths.

### 3. Resolve serials and metadata

For each candidate source file:

- if `.iso`, download to the work directory and run `bsdtar -x -f ... -O
  ./.disk/info`; parse the line; validate; get the serial;
- otherwise use the source mtime, grouped by artifact identity for the daily
  counter;
- for companion files, copy the serial of the artifact they accompany;
- for checksum files, do not create a target; remember the source bytes and the
  directory's artifact list.

### 4. Translate to canonical targets

For each source file produce one or more `Target` records:

- artifact target: container/object;
- checksum targets: one `.sha256` and one `.sha256.gpg` per artifact in the
  directory;
- companion target: same serial as the artifact.

Deduplicate targets by `(container, object)`. If two source paths map to the same
target, keep the first deterministic source (prefer the version directory over the
codename directory, or sort by path).

### 5. Add-only gate

For every target:

- `storage.Stat(ctx, container, object)`; if present, skip;
- optionally cross-check `index.Lookup(container, object)` and the vfs `View`.

Do not use `View.Resolve` as the gate: it is lossy and cannot distinguish multiple
serials of the same release.

### 6. Download and upload

For each missing target:

- download the source bytes over HTTP to the work directory;
- validate size and SHA256 against the source `SHA256SUMS` when available;
- upload with `swift upload <container> <file> --object-name <object>`;
- use the configured content type/headers as needed;
- remove the staging file on success;
- record the result.

For checksum targets, upload the same source bytes under each canonical artifact's
`.sha256`/`.sha256.gpg` name. Do not split or rewrite the checksum file.

### 7. Refresh and verify

- `index.Refresh(ctx, storage)`.
- Rebuild the views.
- For every source path planned, call `LayoutCDImage.ResolvePath(sourcePath)` and
  verify it resolves to the expected canonical object.
- For checksum paths, verify the synthetic `SHA256SUMS`/`SHA256SUMS.gpg` resolves.
- For core `current`/dated paths, verify the alias resolves.
- For server/WSL, verify the released `ubuntu/releases/...` projection.
- For netboot tarballs, verify the ordinary release artifact projection.

Verification failures are reported as errors; the sync never deletes or rolls back
existing objects.

## Add-only semantics

- No deletes.
- No updates.
- If the canonical object exists, skip it.
- Re-running the binary is idempotent.
- A new build of the same point release gets a new serial and therefore a new
  canonical object.
- Rebuilding the same serial is ignored; the user must change the serial or
  manually remove the object to force a replacement.
- The vfs serial-aware selection ensures the newest build is served without
  deleting older builds.

## Testing

### Unit tests

- `internal/images`: serial parsing, canonical/legacy spelling, extension split,
  `+` architecture variants, artifact parsing.
- `internal/vfs`: server/WSL released projection, netboot tarball projection,
  core stable projection, checksum duplication, serial-aware newest selection,
  round-trip canonical → virtual → canonical.
- `internal/synchronizer/cdimage`: source path parsing, product mapping,
  server/WSL special case, core, netboot tarballs, companion files, checksum
  groups, deduplication, serial assignment from mtime.
- `cmd/cdimage-sync`: config parsing, dry-run plan, add-only gate, upload command
  construction, verification.

### Integration tests

- A local fake source tree served by `httptest` and a local rsync listing fixture.
- A fake `bsdtar` and `swift` executable in `PATH` that record commands and create
  the expected canonical files in a fake Swift directory.
- A fake storage/index to verify the vfs projection after upload.
- Cases:
  - desktop release with version and codename directories;
  - server live/preinstalled and WSL;
  - core stable with dated/current;
  - netboot tarball with no expansion;
  - checksum duplication and synthetic folder-level checksums;
  - multiple serials of the same release;
  - add-only rerun skips everything;
  - dry-run performs no downloads/uploads.

## Implementation milestones

1. **Parser and serial support**
   - `images.Serial`, `ParseArtifact` extension, `validArchitecture` variants.
   - Tests.

2. **VFS contract**
   - server/WSL released projection;
   - netboot tarball as ordinary artifact;
   - serial-aware newest selection;
   - checksum/latest serial handling;
   - round-trip tests.

3. **Translator**
   - pure source-path → canonical-target translation;
   - server/WSL, core, netboot tarballs, companions, checksums;
   - serial from metadata;
   - tests.

4. **Source inventory**
   - rsync list parser and filters;
   - version/codename deduplication;
   - dry-run plan.

5. **Metadata and download**
   - HTTP download;
   - `bsdtar` `.disk/info` parsing;
   - mtime fallback and counter assignment;
   - checksum parsing/validation.

6. **Upload and verification**
   - `swift upload` command construction;
   - storage HEAD/stat add-only gate;
   - index refresh and vfs verification;
   - integration tests.

7. **CLI/config/docs**
   - `cmd/cdimage-sync`;
   - `[cdimage_sync]` config;
   - README/docs updates;
   - packaging if needed.

## Open questions / risks

1. **ISO `.disk/info` variants**: the exact line format may vary between releases.
   The parser should be tolerant and validate the fields it can.
2. **Checksum validation**: the source `SHA256SUMS` may include filenames that are
   not mirrored (e.g. torrents or expanded netboot files). The validator should
   only check files it plans to upload and should not fail on missing optional
   companions.
3. **GPG verification**: the sync cannot verify the signature without the public
   key. It duplicates the signed file and its `.gpg`; verification is out of scope.
4. **Milestone/beta releases**: the source releases tree may contain milestone
   releases. The translator supports them, but the include filter should state
   whether they are mirrored.
5. **Content type**: the canonical object's content type should be preserved from
   the source HTTP response or inferred from the extension. The vfs does not
   currently depend on it, but Swift clients may.
6. **Large objects**: `swift upload` may need SLO/segment options for multi-GB
   ISOs. The command builder should allow passing extra Swift CLI arguments.
7. **Staging space**: the pipeline downloads ISOs to a work directory. The work
   directory must have enough space and cleanup must be reliable.
8. **Source symlinks**: core `current` and release aliases may be symlinks in the
    rsync listing. The sync should resolve them to the dated/version target and
    avoid uploading the alias as a separate object.

## Summary

The sync is a thin orchestrator around `rsync`, HTTP, `bsdtar`, and `swift upload`.
The vfs is used as the grammar and verification layer, not as a writable
filesystem. The reverse translator is the new core; it reuses the vfs's forward
rules and is guarded by round-trip tests. The vfs needs three targeted changes:
server/WSL released projection, serial-aware newest selection, and broader
architecture validation. The sync is add-only, mirrors only released artifacts,
stores netboot tarballs without expansion, and duplicates checksums per artifact so
the existing synthetic folder-level checksum aliases keep working.
