#!/usr/bin/env bash
# CI selects go-version-file: go.mod. Check the compiler actually running,
# instead of parsing a second version literal out of the workflow YAML.
set -euo pipefail
GOMOD_GO="$(go mod edit -json | python3 -c 'import sys,json; print(json.load(sys.stdin)["Go"])')"
ACTUAL_GO="$(go env GOVERSION)"
ACTUAL_GO="${ACTUAL_GO#go}"
GOMOD_MINOR="$(printf '%s' "$GOMOD_GO" | cut -d. -f1,2)"
ACTUAL_MINOR="$(printf '%s' "$ACTUAL_GO" | cut -d. -f1,2)"
if [[ "$GOMOD_MINOR" != "$ACTUAL_MINOR" ]]; then
  echo "::error::Go version mismatch: go.mod requires $GOMOD_GO; compiler is $ACTUAL_GO"
  exit 1
fi
printf 'Go version consistent: go.mod=%s, compiler=%s\n' "$GOMOD_GO" "$ACTUAL_GO"
