# XDev domain glossary

Single-context vocabulary for contributors and architecture reviews. Prefer these names over synonyms.

## Product

- **XDev** — cross-platform development-environment version manager. One distributed `xdev[.exe]` runs CLI, TUI, GUI and managed-tool shim dispatch. No separate GUI executable is distributed.
- **Desktop build** — the distributed binary built with `desktop,production` (Linux also `gtk3`). It embeds frontend assets and uses Wails v3.1.0. Project scripts delegate to `node scripts/build.mjs`.
- **Maintenance build** — a build without `desktop`: CLI/TUI/shim code remains available, while GUI returns an explicit unavailable-runtime error. It does not require frontend/dist. It is not the distributed desktop product.
- **Shim** — a hard link (Windows) or symlink (Unix) whose basename is a tool alias; the same executable dispatches to the real tool. Unix uses `unix.Exec`; Windows copying is a fallback when hard-link creation fails. Zero-argument tool shims dispatch before the GUI branch.
- **Manifest** — YAML tool entry describing versions, archives, shims, environment and form factor.
- **Plugin** — manifest-to-install/archive adapter (Generic / Lua / Vfox / Java). The installer owns downloading, verification, installation and activation.

## Catalog

- **Catalog** — in-memory tool collection. Disk loading and cache fingerprinting share the top-level plus `third-party/` source set. A directory containing only third-party manifests is not empty and does not trigger repository fallback.
- **Disk precedence** — top-level manifests load first, then third-party manifests; the later same-name disk entry wins. Embedded manifests then retain trusted fields of their existing versions; disk-only versions may be appended.
- **Latest promotion** — a disk latest must exist in the merged list, be installable and newer than the embedded effective default. Stable defaults cannot advance to prereleases; prereleases may advance within the same channel or to a newer stable release. Arbitrary labels do not establish upgrade ordering. `DefaultVersion` consumers use the accepted default, including the original LTS/first-entry fallback.
- **Catalog fingerprint** — schema-2 identity over platform, source, relative paths and SHA-256 of the same top-level/third-party byte snapshots that are parsed. Content changes invalidate across processes even when size and mtime are unchanged; unreadable sources cannot produce a falsely fresh cache.

## Host scan

- **Host-scan** — `internal/hostscan` walks a PATH snapshot and returns every **Hit**, skipping the xdev shim directory. Each PATH directory is read once. Callers pass ShimDir from resolved configuration; helpers do not reload default home.
- **Hit** — `{Path, Dir}` for a binary found on PATH.
- **Managed instance** — a Hit inside InstallDir or ShimDir.
- **Local instance** — a Hit outside managed directories. GUI local detection builds one basename index and matches catalog tools against it.

## Install session

- **InstallSpec** — session input: sessionId, tool, version, form, confirmed, force, kind, method and pmDigest. pmDigest approves the exact local/third-party PM definition; it does not bypass session/resource/cancellation coordination.
- **InstallSession** — authoritative backend snapshot identified by a unique sessionId, independent of tool/version display fields. A monotonic revision lets the UI ignore reordered snapshots. The resource key prevents concurrent writers to the same resource.
- **Acceptance** — `StartInstall` returns an identity, not installation success. The frontend subscribes before starting and queries `GetInstallSession` to recover fast completion or uncertain IPC responses.
- **Terminal outcome** — completed, error, cancelled or needs-repair. A session terminates once; needs-repair represents an existing incomplete installation. Retrying uses a new identity.
- **Cancellation** — `CancelInstall` acknowledges a request. The session remains cancelling until the worker returns. BeforeCommit arbitrates cancelling versus committing under the same lock; once committing wins, user cancellation is unavailable. Clients use canCancel/canPause instead of guessing from phase text.
- **Legacy install binding** — `InstallTool*`, `RepairInstall`, `InstallPackageManager` and `InstallSubTool` retain their callable interfaces and wait for the real session outcome.
- **Kind** — tool (catalog pipeline), package-manager (official script or catalog PM), sub-tool (corepack / pip / uv / npm method).
- **Sub-tool** — enabled or installed through a parent tool rather than an independent catalog entry, such as yarn/pnpm/pipx. Resource ownership follows the writer, such as node for corepack/npm.
- **Install coordinator** — frontend ownership shared by single install, repair, PM and batch preflight/confirmation. A batch owns one lease and creates independent backend sessions for its items.

## Desktop adapter

- **App** — exported Go service facade. Framework operations live behind desktopRuntime; business methods use application/session contexts.
- **Wails binding** — generated Wails v3 callable under `frontend/bindings`. Generate with desktop plus bindings tags so a static asset stub avoids requiring frontend/dist. Production desktop builds omit bindings and embed real assets.
- **Desktop bridge** — `frontend/src/desktop/bridge.js` installs the compatibility namespaces used by existing pages. Generated bindings remain the production caller; Events.On envelopes are normalized from event.data and subscriptions have explicit disposal.
- **Lifecycle** — ServiceStartup creates the app root context; WindowRuntimeReady shows the window and starts DOM-dependent work; ServiceShutdown cancels application work and performs cleanup.
- **RunOptions** — carries the already-resolved CLI configuration policy into GUI. `--xmdevenv-dir` is not discarded by reloading default config; NoEnvWrite comes from `--no-path-fix` and gates host environment writes.

## Decisions and verification

Wails v3.1.0 migration is authorized by the user's approval of Spec 040; ADR-0007 records the decision. The v3 configuration is wails.yaml; project build scripts remain the supported build/release route. Direct wails3 build is not equivalent to the signing pipeline, and direct wails3 dev has not been validated in this iteration.

The integration lead reported Windows desktop build and native WebView2 rendering, generated bindings, GetConfig with an isolated temporary home, and exit checks passed. Linux manifest/plugin/gui/installer package tests and the desktop,production,gtk3 build passed. macOS has not been run on a real host. These are scoped verification results, not a claim that every release/platform check is complete.

## Avoid

- Do not call host-scan a service or API.
- Do not treat yarn/pnpm as standalone catalog tools.
- Do not equate StartInstall or CancelInstall acknowledgement with terminal success/cancellation.
- Do not distribute a maintenance build as the complete desktop binary.
- `one-api/` is unrelated legacy checkout residue, not part of XDev; see THIRD_PARTY.md.
