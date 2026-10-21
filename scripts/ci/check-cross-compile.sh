#!/usr/bin/env bash
# Cross-platform test compilation gate.
#
# Why this script exists (20261021 audit, P1-1):
# `go build ./...` only compiles production files. Windows-only symbols
# referenced from an untagged _test.go are therefore invisible on Windows
# and only blow up when CI runs `go test` on Linux/macOS. `go vet` compiles
# test files too, so a cross-GOOS vet is the cheapest way to catch that.
#
# Prerequisite: frontend/dist must exist (frontend/embed.go uses
# `//go:embed all:dist`). CI builds the frontend before this step; locally
# run `cd frontend && npm run build` once if it is missing.
set -euo pipefail

targets="${XDEV_CROSS_COMPILE_TARGETS:-linux/amd64 darwin/arm64}"

for target in $targets; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  echo "== go vet GOOS=${goos} GOARCH=${goarch} =="
  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 go vet ./...
done

echo "cross-platform test compilation: OK"
