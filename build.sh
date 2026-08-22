#!/bin/bash
# Build script for xdev on Linux/macOS
set -e

VERSION=$(grep -oP 'Version = "\K[^"]+' internal/version/version.go)
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

echo "Building xdev v${VERSION}..."
mkdir -p build/bin
go build -ldflags "$LDFLAGS" -o build/bin/xdev .

# Audit download sources (release gate) before GUI build
if [ "$SKIP_AUDIT" != "1" ]; then
    echo "Running download source audit (probe gate)..."
    if ! ./scripts/audit-links.sh; then
        echo "[ERROR] download source audit failed. Set SKIP_AUDIT=1 to bypass." >&2
        exit 1
    fi
fi

echo "Building xdev-gui v${VERSION}..."
wails build -tags gui -ldflags "$LDFLAGS" -o build/bin/xdev-gui

cp build/bin/xdev "build/bin/xdev-${VERSION}"
cp build/bin/xdev-gui "build/bin/xdev-gui-${VERSION}"
echo "Done!"
