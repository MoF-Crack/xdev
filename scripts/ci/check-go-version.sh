#!/usr/bin/env bash
# Spec 001: Verify CI Go version matches go.mod's go directive.
# Exits non-zero with a diagnostic if they differ at the minor level.
set -euo pipefail

GOMOD_GO="$(go mod edit -json | python3 -c 'import sys,json; print(json.load(sys.stdin)["Go"])' 2>/dev/null || grep '^go ' go.mod | head -1 | awk '{print $2}')"
CI_GO="$(grep 'go-version:' .github/workflows/ci.yaml | head -1 | sed 's/.*go-version: *"\([^"]*\)".*/\1/')"

# Normalize: strip patch, keep major.minor
GOMOD_MINOR="$(echo "$GOMOD_GO" | cut -d. -f1,2)"
CI_MINOR="$(echo "$CI_GO" | cut -d. -f1,2)"

if [ "$GOMOD_MINOR" != "$CI_MINOR" ]; then
  echo "::error::Go version mismatch: go.mod requires $GOMOD_GO but CI uses $CI_GO"
  echo "::error::Fix: update go-version in .github/workflows/ci.yaml to match go.mod"
  exit 1
fi

echo "Go version consistent: go.mod=$GOMOD_GO, CI=$CI_GO"
