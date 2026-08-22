# XDev domain glossary

Single-context vocabulary for architecture skills. Prefer these names over synonyms.

## Product

- **XDev** — cross-platform development-environment version manager (CLI + TUI + GUI). The **CLI binary** is also every managed-tool **shim**. The GUI is a separate `xdev-gui` binary (`-tags gui`).
- **Shim** — a hard link (Windows) or symlink (Unix) whose basename is a tool alias; the CLI binary dispatches to the real tool. Unix uses `unix.Exec` (process overlay). Windows copies are a fallback when `CreateHardLink` fails.
- **Manifest** — YAML catalog entry for a tool (versions, archives, shims, form factor).
- **Catalog** — in-memory load of all manifests.
- **Plugin** — adapter that turns a manifest into install/archive behaviour (Generic / Lua / Vfox / Java).

## Host scan

- **Host-scan** — one module (`internal/hostscan`) that walks a PATH snapshot and returns every **Hit**, skipping the xdev **shim directory**. Each PATH directory is `ReadDir` once (Spec 031). Callers pass `ShimDir` from App config; they never `config.Load("")` inside helpers. GUI `refreshLocalCache` builds one basename index then matches every catalog tool against it. `env.ScanLocalTools` PATH lookup uses hostscan, not `exec.LookPath`.
- **Hit** — `{Path, Dir}` for one binary found on PATH.
- **Managed instance** — a Hit whose path is inside xdev `InstallDir` or `ShimDir`.
- **Local instance** — a Hit outside xdev directories (system leftover, e.g. D: pnpm while E: Node is xdev-managed).

## Install session

- **InstallSpec** — single install-session input: tool, version, form, confirmed, force, kind, method. Wails wrappers (`InstallTool*`, `InstallPackageManager`, `InstallSubTool`) forward into `App.install(spec)` and are not new bindings.
- **Kind** — `tool` (catalog pipeline), `package-manager` (official script or catalog PM), `sub-tool` (corepack enable / pip|uv|npm method).
- **Package manager** — npm/yarn/pnpm/pip/uv/scoop/brew/choco etc. Independent PMs have `detectCmd == toolName`; **sub-tools** (yarn/pnpm/pipx) do not.
- **Sub-tool** — a package manager that is enabled/installed through a parent tool (Node corepack, Python pip), not a standalone catalog entry.

## Desktop adapter

- **App** — Wails desktop adapter. Exported methods stay on App so JS bindings do not grow; implementation groups into catalog query, install session, host-scan, prefs/env.
- **Wails binding** — an exported `App` method generated into `frontend/wailsjs`. Spec 029/031 do not add bindings. `localCacheUpdated` is a Wails event, not a new method.

## Avoid

- Do not call host-scan a “service” or “API”.
- Do not treat yarn/pnpm as standalone catalog tools.
- `one-api/` is not part of XDev. It is gitignored leftover from an older checkout; Spec 029 B5 evicts it from the working tree. See `THIRD_PARTY.md`.
