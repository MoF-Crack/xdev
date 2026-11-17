#!/usr/bin/env bash
set -euo pipefail
OS="$1"; ARCH="$2"; VERSION="${3#v}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo 'Invalid version' >&2; exit 1; }
BIN="$PWD/build/bin/xdev"
OUT="$PWD/dist"
mkdir -p "$OUT"
STAGING="$(mktemp -d)"
# STAGING is created here and never supplied by a caller.
trap 'rm -rf -- "$STAGING"' EXIT
if [[ "$OS" == darwin ]]; then
  # Preserve the existing Apple Silicon DMG alongside canonical updater archives.
  if [[ "$ARCH" == arm64 ]]; then
    mkdir -p "$STAGING/dmg"
    cp "$BIN" "$STAGING/dmg/xdev"
    chmod +x "$STAGING/dmg/xdev"
    hdiutil create -volname XDev -srcfolder "$STAGING/dmg" -ov -format UDZO "$OUT/xdev-darwin-arm64-v${VERSION}.dmg"
  fi
  exit 0
fi
[[ "$OS" == linux ]] || { echo "Unsupported package OS: $OS" >&2; exit 1; }
case "$ARCH" in
  amd64) APPARCH=x86_64; HASH=ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0 ;;
  arm64) APPARCH=aarch64; HASH=f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158 ;;
  *) echo 'Unsupported architecture' >&2; exit 1 ;;
esac
APPDIR="$STAGING/XDev.AppDir"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/256x256/apps"
cp "$BIN" "$APPDIR/usr/bin/xdev"
chmod +x "$APPDIR/usr/bin/xdev"
cat > "$APPDIR/xdev.desktop" <<'DESKTOP'
[Desktop Entry]
Name=XDev
Comment=Cross-platform development environment manager
Exec=xdev
Icon=xdev
Terminal=false
Type=Application
Categories=Development;
DESKTOP
cp "$APPDIR/xdev.desktop" "$APPDIR/usr/share/applications/xdev.desktop"
cp build/appicon.png "$APPDIR/xdev.png"
cp build/appicon.png "$APPDIR/.DirIcon"
cp build/appicon.png "$APPDIR/usr/share/icons/hicolor/256x256/apps/xdev.png"
cat > "$APPDIR/AppRun" <<'APPRUN'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
exec "$HERE/usr/bin/xdev" "$@"
APPRUN
chmod +x "$APPDIR/AppRun"
curl -fL --retry 3 "https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-${APPARCH}.AppImage" -o "$STAGING/appimagetool"
printf '%s  %s\n' "$HASH" "$STAGING/appimagetool" | sha256sum -c -
chmod +x "$STAGING/appimagetool"
ARCH="$APPARCH" "$STAGING/appimagetool" --appimage-extract-and-run "$APPDIR" "$OUT/xdev-${VERSION}-${APPARCH}.AppImage"
if [[ "$ARCH" == amd64 ]]; then
  sudo apt-get update -qq
  sudo apt-get install -y -qq ruby ruby-dev rpm
  sudo gem install fpm --version 1.16.0 --no-document
  PKGROOT="$STAGING/pkg"
  mkdir -p "$PKGROOT/usr/bin"
  cp "$BIN" "$PKGROOT/usr/bin/xdev"
  chmod +x "$PKGROOT/usr/bin/xdev"
  fpm -s dir -t deb -n xdev -v "$VERSION" -a amd64 --license MIT --depends libgtk-3-0 --depends libwebkit2gtk-4.1-0 --url https://github.com/MoF-Crack/xdev -C "$PKGROOT" -p "$OUT/xdev-${VERSION}-linux-amd64.deb"
  fpm -s dir -t rpm -n xdev -v "$VERSION" -a x86_64 --license MIT --depends gtk3 --depends webkit2gtk4.1 --url https://github.com/MoF-Crack/xdev -C "$PKGROOT" -p "$OUT/xdev-${VERSION}-linux-amd64.rpm"
fi
