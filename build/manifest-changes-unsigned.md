# Manifest Schema Changes — Install Kind (Unsigned)

**Date:** 2026-07-14  
**Status:** Unsigned — manifest signature not regenerated (see Safety Analysis below)

## Summary

Extended the `Archive` schema with an `install_kind` field that lets the
installer distinguish extractable archives from self-installing
executables and shell scripts. This enables correct handling of Rust's
`rustup-init.exe` (executable) and `sh.rustup.rs` (script), which were
previously treated as archives and would have been passed to the
extractor instead of being run.

## Schema Extension

### New field: `Archive.InstallKind`

```yaml
archives:
  - os: windows
    format: exe
    url: https://static.rust-lang.org/rustup/dist/.../rustup-init.exe
    sha256: 0cba64...
    install_kind: executable      # ← NEW
```

| Value         | Behaviour                                                        |
|---------------|------------------------------------------------------------------|
| `archive`     | Default. Extract the file (zip/tar.gz/...) into the install dir. |
| `executable`  | Verify SHA-256, then run the cached binary with `install_args`. |
| `script`      | Verify SHA-256, then execute via `sh <cached-script> <install_args>`. |
| *(empty)*     | Treated as `archive`.                                             |
| *(unknown)*   | Falls back to `archive` (safe default — never executes code).    |

### Go API

- `manifest.InstallKindArchive = "archive"`
- `manifest.InstallKindExecutable = "executable"`
- `manifest.InstallKindScript = "script"`
- `Archive.InstallKind string` (yaml tag: `install_kind,omitempty`)
- `func (a Archive) EffectiveKind() string` — returns the kind, defaulting
  to `"archive"` for empty or unrecognised values. Only `"executable"`
  and `"script"` pass through; everything else collapses to the safe
  `"archive"` default.

### Trust model

Unknown values **never** silently become `executable` or `script`.
Running downloaded code requires explicit opt-in via the manifest. This
prevents a typo or a future kind string from accidentally enabling code
execution.

## Installer Routing

### `installByKind` (new method)

After download, `InstallWithProgress` calls `installByKind` instead of
`extract` directly:

```
installByKind(ctx, p, arch, ver, installPath)
  ├── kind = resolveKind(arch)
  │     ├── arch.EffectiveKind()           # explicit manifest value
  │     └── heuristic fallback (see below) # for manifests without kind
  ├── archive    → i.extract(...)           # existing extraction path
  ├── executable → i.runInstaller(...)      # SHA verify + run binary
  └── script     → i.runInstaller(...)      # SHA verify + run via "sh"
```

### `runInstaller` (new method)

1. Locate the cached file at `<cacheDir>/<tool>-<ver>.<format>`.
2. **Mandatory SHA-256 verification** — refuses to run if SHA is empty,
   all-zero (placeholder), or doesn't match the cached file. This is a
   defense-in-depth check (the download stage also verifies).
3. Create the install directory.
4. Build the command:
   - `executable`: run `<cached-exe> <install_args>`
   - `script`: run `sh <cached-script> <install_args>`
5. Overlay plugin env vars (e.g. `CARGO_HOME`, `RUSTUP_HOME`) on the
   inherited environment.
6. Execute via the injectable `execRunner` interface (mockable in tests).

### Heuristic fallback (`resolveKind`)

When `install_kind` is not declared (empty), `resolveKind` inspects the
URL for well-known installer patterns:

| URL pattern         | Inferred kind  |
|---------------------|----------------|
| `sh.rustup.rs`      | `script`       |
| `rustup-init.exe`   | `executable`   |

This lets older manifests benefit from the routing without re-signing.
The heuristic only fires when `install_kind` is empty — an explicit
`archive` value is always respected.

### Testability

- `execRunner` interface with `Run(ctx, name, args, env, dir)` method.
- `osExecRunner` is the production implementation (uses
  `exec.CommandContext`).
- `Installer.SetExecRunner(r)` injects a mock for tests.
- All routing tests use `mockExecRunner` — no real commands are executed.

## rust.yaml Changes

Added `install_kind` to every archive across all 3 versions
(1.82.0, 1.81.0, 1.80.1):

| OS      | Format | install_kind  |
|---------|--------|---------------|
| windows | exe    | `executable`  |
| linux   | sh     | `script`      |
| darwin  | sh     | `script`      |

**SHA-256 values unchanged.** Only the `install_kind` field was added.

### Why `sh.rustup.rs` is a script, not an archive

`https://sh.rustup.rs` is a rolling URL that always serves the latest
`rustup-init.sh` shell script. Treating it as an archive would pass it
to the extractor (which expects zip/tar/etc.), failing. With
`install_kind: script`, the installer verifies its SHA-256 and runs it
via `sh`, which is the correct behaviour.

## Signature Safety Analysis

The manifest signature payload (see `signer.signaturePayload`) is:

```
tool + "\n" + sha256_map + "\n" + timestamp(RFC3339)
```

The `install_kind` field is **not** part of the payload. Adding
`install_kind` to `rust.yaml` therefore does **not** invalidate the
existing signature — the signed SHA-256 map is unchanged.

**However**, the signature has not been regenerated for the updated
`rust.yaml`. The file is now `build/manifest-changes-unsigned.md`
(documenting this fact). A maintainer with the private Ed25519 key
should re-sign `manifests/rust.yaml` at the next release.

### What changed in the signed payload

Nothing. The `sha256_map` JSON (map of version → os/arch → sha256) is
identical because only `install_kind` was added, not any SHA-256 value.

## Files Modified

| File                                    | Change                                              |
|-----------------------------------------|-----------------------------------------------------|
| `internal/manifest/manifest.go`         | Added `InstallKind` field, constants, `EffectiveKind()` |
| `internal/manifest/manifest_test.go`    | Added 5 `EffectiveKind` tests (default, archive, executable, script, unknown) |
| `internal/manifest/installkind_test.go` | Removed (consolidated into `manifest_test.go`)      |
| `internal/installer/installer.go`        | Added `execRunner`, `osExecRunner`, `SetExecRunner`, `resolveKind`, `installByKind`, `runInstaller`; wired `InstallWithProgress` → `installByKind` |
| `internal/installer/kind_routing_test.go` | New: 5 routing tests (archive, executable, script, SHA mismatch, placeholder SHA) |
| `manifests/rust.yaml`                   | Added `install_kind: executable` (windows) and `install_kind: script` (linux/darwin) to all archives; SHA-256 values unchanged |
| `build/manifest-changes-unsigned.md`    | This document                                       |

---

# Java Manifest — Oracle Template Replaced with Temurin Pinned Assets

**Date:** 2026-07-14  
**Status:** Unsigned — no prior `java.yaml.sig` existed; SHA-256 map is new

## Summary

Replaced the Oracle JDK template manifest (which had empty `archives: []`
placeholders and relied on dynamic URL generation in the Java plugin) with
25 pinned Adoptium Temurin GA archives across 5 major versions and 5 platforms.
The Java plugin no longer synthesises download URLs — it reads fixed archives
from the manifest, ensuring every download is verified against a trusted
SHA-256 hash.

## What Changed

### `manifests/java.yaml`

| Before                                    | After                                       |
|-------------------------------------------|---------------------------------------------|
| `display_name: Oracle JDK`                | `display_name: Eclipse Temurin`             |
| `homepage: oracle.com/...`                | `homepage: adoptium.net`                    |
| Oracle URL template comments (15 lines)   | Removed                                     |
| `remote: oracle/graal` section            | Removed (fixed assets, no remote discovery) |
| `versions[].archives: []` (all empty)     | 25 real archives with URL + SHA-256         |
| 6 versions (8/11/17/21/25/26)            | 5 versions (11/17/21/25/26) — Java 8 dropped |

**5 majors × 5 platforms = 25 archives:**

| Major | Temurin Version | Platforms                          |
|-------|-----------------|------------------------------------|
| 11    | 11.0.31+11      | win/amd64, linux/x86_64, linux/arm64, darwin/x86_64, darwin/arm64 |
| 17    | 17.0.19+10      | same 5 platforms                   |
| 21    | 21.0.11+10      | same 5 platforms                   |
| 25    | 25.0.3+9        | same 5 platforms                   |
| 26    | 26.0.1+8        | same 5 platforms                   |

### `internal/plugins/java/java.go`

**Before:** Dynamic URL generation via `oracleJDKURL()` / `adoptiumURL()` API
endpoint. Versions without pinned SHA-256 in the manifest would download
without hash verification (security gap).

**After:** `ArchiveForPlatform(version, os, arch)` looks up pinned archives
directly from the manifest. `ArchiveFor(version)` delegates to the host
platform. Empty archives (placeholders) return an error — **no pinned SHA,
no download**.

Removed functions: `oracleJDKURL`, `adoptiumURL`, `isNFTC`, `sourceName`,
`otnLicenseCookie`.

Added method: `ArchiveForPlatform(version, os, arch string) (*manifest.Archive, error)`

### `internal/plugins/java/java_test.go`

Replaced old unit tests (for deleted dynamic-URL functions) with 7 tests:
interface compliance, pinned Temurin for Java 17 & 21, all-platforms
coverage, 64-hex SHA validation, placeholder error, unknown version error,
and `ArchiveFor` delegation.

### `internal/plugins/java/java_manifest_test.go`

Consolidated into `java_test.go`; file reduced to package declaration.

## Signature Impact

No `manifests/java.yaml.sig` file previously existed — the Java manifest was
never signed. The SHA-256 map is entirely new (25 real hashes), so there is
no prior signature to invalidate.

A maintainer with the private Ed25519 key should sign `manifests/java.yaml`
at the next release to establish the trust chain for these pinned hashes.

## Files Modified

| File                                          | Change                                              |
|-----------------------------------------------|-----------------------------------------------------|
| `manifests/java.yaml`                         | Replaced Oracle template with 25 pinned Temurin archives; updated metadata to Temurin; removed remote section |
| `internal/plugins/java/java.go`               | Rewrote: removed dynamic URL generation; added `ArchiveForPlatform`; placeholder archives now error |
| `internal/plugins/java/java_test.go`           | Replaced dynamic-URL function tests with 7 manifest-driven tests |
| `internal/plugins/java/java_manifest_test.go`  | Consolidated into `java_test.go`; package declaration only |
| `build/manifest-changes-unsigned.md`          | Appended Java section                               |
