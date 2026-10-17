#!/bin/bash
# Build script for xdev on Linux/macOS（单二进制模型：GUI+TUI+CLI 一个文件）
set -e

# F-11: POSIX-portable version extraction. The previous form
# `grep -oP 'Version = "\K[^"]+'` is GNU-only: BSD/macOS grep has no -P (PCRE)
# support and aborts with "invalid option -- P", which aborted build.sh on
# macOS before it built anything. sed BRE (s///p) and `head -n 1` are in the
# POSIX subset, so GNU sed (Linux/CI) and BSD sed (macOS) behave identically.
VERSION=$(sed -n 's/^[[:space:]]*const Version = "\([^"]*\)".*$/\1/p' internal/version/version.go | head -n 1)
if [ -z "$VERSION" ]; then
    echo "[ERROR] cannot parse Version from internal/version/version.go" >&2
    exit 1
fi

LDFLAGS="-s -w -X github.com/xmdemt/xdev/internal/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
SKIP_AUDIT="${SKIP_AUDIT:-0}"

# Security (C-5): Inject the Ed25519 public key for manifest signature
# verification. When embedded, the downloader refuses unsigned/all-zero
# manifests, protecting against MITM attacks.
if [ -f "keys/pub.key" ]; then
    PUB_KEY=$(cat keys/pub.key | tr -d '\n\r ')
    LDFLAGS="$LDFLAGS -X github.com/xmdemt/xdev/internal/signer.rootPublicKeyB64=$PUB_KEY"
    echo "[security] Embedding Ed25519 public key for manifest signature verification"
else
    echo "[security] WARNING: keys/pub.key not found — manifest signature verification will be DISABLED"
fi

# Audit download sources (release gate)
if [ "$SKIP_AUDIT" != "1" ]; then
    echo "Running download source audit (probe gate)..."
    if ! ./scripts/audit-links.sh; then
        echo "[ERROR] download source audit failed. Set SKIP_AUDIT=1 to bypass." >&2
        exit 1
    fi
fi

echo "Building xdev v${VERSION} (GUI+TUI+CLI single binary)..."
mkdir -p build/bin
wails build -ldflags "$LDFLAGS" -o build/bin/xdev

cp build/bin/xdev "build/bin/xdev-${VERSION}"
echo "Done!"