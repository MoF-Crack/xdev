# Changelog

All notable changes to XDev are documented in this file.
Format based on [Keep a Changelog](https://keepachangelog.com/), adheres to [SemVer](https://semver.org/).

## [Unreleased]

### Fixed
- Ignore-list tests no longer write into the real `$XMDEV_HOME/cache/ignored_tools.json` (Go reappearing after `go test`)
- Sniff-discovered versions (all-zero SHA-256) no longer fail install with "hash tampering" against the signed manifest
- Sniff completion badge clears after the notification panel is opened
- Install confirmation no longer waits on a full category scan / catalog reload (was >3s)
- Yarn/pnpm install methods now detect xdev-managed Node (`corepack` / `npm` on E:) even when PATH still points at a stale D: copy; enable/install stays available for the xdev Node
- Available versions (OpenCode sniff list) sort by release date then semver, newest first — sniffed versions are no longer stuck at the bottom of YAML-append order
- Installed pipx no longer keeps an Install button; yarn/pnpm only keep Enable/Install when a local copy exists but xdev Node has not enabled them yet
- Package-manager install/enable/update now lives in the tool detail drawer, matching language/IDE/build tools; catalog rows only open detail and favorite
- `UpdatePackageManager` no longer treats yarn/pnpm as standalone catalog tools (was `InstallTool("pnpm")`); HasUpdate is not set for sub-tools
- Local-scan ready signal fires after disk cache load; host scan starts immediately instead of waiting 15s
- Local PATH detection uses the App config shim dir (no extra `config.Load("")`)
- Windows shims recopy when size matches but mtime differs (upgrade no longer skipped)
- Host-scan module (`internal/hostscan`) skips xdev shim copies on PATH and returns every PATH instance (not just `LookPath` first hit)
- Windows shims prefer a hard link to xdev.exe (fallback copy)
- GUI install wrappers share one `InstallSpec` (`tool` / `package-manager` / `sub-tool`); App stays a Wails adapter over catalog, install-session, host-scan, prefs

## [0.3.1] - 2026-08-18

### Fixed
- npm sniff no longer fails on large packuments (opencode-ai): request abbreviated registry metadata
- Archive URL templates now cover tag-only GitHub downloads (uv / ninja / zed), so sniffed versions get installable URLs
- Yarn/pnpm can be installed via `npm install -g` as well as `corepack enable`
- Unignore by name+kind drops all matching rows; local-scan results refresh after unignore

## [0.3.0] - 2026-08-18

### Added
- Three-round security hardening: isolate/remote command allowlist, proxy URL validation, profile env-var whitelist, Lua `load()` sandbox, SSH target sanitization
- Lua plugin loader now accepts nested `PLUGIN.available` / `PLUGIN.archive` templates
- Windows PATH cleanup removes leftover `TestInitConfig_NoDeadlock` entries (case-insensitive)

### Changed
- `klauspost/compress` 1.17.11 → 1.18.7
- Frontend `nanoid` 3.3.16 → 3.3.18

### Security
- SSRF redirect hops re-validated; DNS resolution fail-closed
- Git clone rejects `http://` / `git://` for third-party manifests
- SSH password passed via `SSHPASS` instead of process arguments

## [0.2.0] - 2026-07-25

### Added
- CI integration tests gate: `go test -tags=integration ./...` on three platforms (Spec 002)
- CI Go version consistency check: `scripts/ci/check-go-version.sh` (Spec 001)
- Release signing hard gate: "Verify signing secrets" step fails if keys missing (Spec 003)
- Build provenance: GitCommit and BuildDate injected via ldflags (Spec 004)
- SHA256SUMS checksum files for release artifacts (M2)
- macOS AMD64 GUI DMG (`xdev-gui-darwin-amd64-v0.2.0.dmg`) (M1)
- GitHub Actions upgraded to v7 (checkout/setup-go/setup-node/upload-artifact) for Node 24
- Go toolchain upgraded to 1.25 (go.mod + CI)
- `internal/version/version.go`: added `GitCommit` variable for ldflags injection

### Changed
- `search` command no-match now returns exit code 0 with message instead of exit code 1 (Spec 007)
- Frontend dependencies: vite 5→6, vitest 2→3, @vue/test-utils downgraded to fix brace-expansion vulnerability
- Go dependencies: golang.org/x/crypto v0.33→v0.54, x/net v0.35→v0.57, x/sys, x/text, x/sync upgraded
- Release workflow: incremental build (skip existing assets), `overwrite_files: false`
- Release workflow: tags trigger (`on.push.tags: ['v*']`)

### Fixed
- Dependabot vulnerabilities: 26→0 (golang.org/x/crypto, x/net, esbuild, postcss, brace-expansion)
- Integration tests cross-platform: `SysProcAttr` type mismatch on non-Windows runners
- `TestFindAsset_NoMatch` false positive on Linux (GOOS/GOARCH substring fallback)
- `TestProbeRangeLengthLimitAndHEADFallback` flaky server-side byte counter (OS-dependent buffering)
- Windows drive-letter tests gated behind `//go:build windows`
- `TestHostArch` platform expectations aligned with Linux/Darwin x86_64 mapping
- ZipSlip absolute-path assertion hardened for cross-platform extractors
- Windows GUI process helpers split behind build tags
- `EnvPair`, `UpdateShimDirInUserPath`, `DetachConsole` Windows API isolation

### Security
- Ed25519 signing key pair configured in GitHub Secrets
- Release artifacts now signed (manifest signatures + public key injection via ldflags)
- All release jobs verify signing secrets before proceeding

## [0.1.0] - 2026-07-24

### Added
- Initial GitHub Actions release with 12 multi-platform artifacts
- Linux: tar.gz (amd64/arm64), deb (amd64/arm64), rpm (amd64/arm64)
- Windows: zip (amd64)
- macOS: tar.gz (amd64/arm64), DMG (arm64 CLI + arm64 GUI)
- AppImage: x86_64 GUI variant
- Incremental release builds (skip existing assets)
- Governance CI with constitution/spec/approvals/links validation
- Spec 005: Host state snapshot implementation (`internal/testkit/hostsnapshot`)
- Governance stabilization Tasks 1-10 completed and archived