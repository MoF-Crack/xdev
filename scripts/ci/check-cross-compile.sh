#!/usr/bin/env bash
# Compile project tests for other platforms without loading the desktop runtime.
# Native desktop compilation is a separate CI matrix step with its system libs.
set -euo pipefail
targets="${XDEV_CROSS_COMPILE_TARGETS:-linux/amd64 darwin/arm64}"
for target in $targets; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  echo "== go vet GOOS=${goos} GOARCH=${goarch} =="
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go vet ./...
done
echo 'cross-platform test compilation: OK'
