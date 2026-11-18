#!/usr/bin/env bash
# Single distributed binary: GUI/TUI/CLI/shim. Uses pinned Wails 3 bindings.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
args=()
if [[ "${SKIP_AUDIT:-0}" == "1" ]]; then args+=(--skip-audit); fi
node "$ROOT/scripts/build.mjs" "${args[@]}" "$@"
