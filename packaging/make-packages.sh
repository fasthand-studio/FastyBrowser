#!/bin/sh
# Builds .deb, .rpm and an AppImage into dist/ from the tarball's contents.
# Needs nfpm (go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest) and
# appimagetool (https://github.com/AppImage/appimagetool). Run make-release.sh first.
set -e
HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$HERE"
VERSION=$(sed -n 's/^[[:space:]]*version *= *"\([0-9][0-9.]*\).*/\1/p' src/store.go | head -1)
ARCH=$(uname -m)
STAGE="$HERE/dist/fasty-browser-$VERSION-linux-$ARCH"
[ -d "$STAGE" ] || { echo "run packaging/make-release.sh first" >&2; exit 1; }
NFPM=${NFPM:-$(command -v nfpm || echo /tmp/claude-1000/bin/nfpm)}
TOOL=${APPIMAGETOOL:-$(command -v appimagetool || echo /tmp/claude-1000/appimagetool)}
OUT="$HERE/dist"
W="$OUT/work"; rm -rf "$W"; mkdir -p "$W/root/usr/lib/fasty-browser" "$W/root/usr/bin" \
  "$W/root/usr/share/applications" "$W/root/usr/share/icons/hicolor/scalable/apps"

# one file layout for the .deb and .rpm: /usr/lib/fasty-browser + /usr/bin link
R="$W/root"
install -m 755 "$STAGE/fasty-browser-bin" "$STAGE/fasty-browser" "$R/usr/lib/fasty-browser/"
[ -f "$STAGE/libfasty-uvm.so" ] && install -m 755 "$STAGE/libfasty-uvm.so" "$R/usr/lib/fasty-browser/"
ln -s ../lib/fasty-browser/fasty-browser "$R/usr/bin/fasty-browser"
cp "$STAGE/fasty-browser.svg" "$R/usr/share/icons/hicolor/scalable/apps/"
install -m 644 "$STAGE/app.fastybrowser.Linux.desktop" "$R/usr/share/applications/"
install -m 644 LICENSE "$W/LICENSE"

case "$ARCH" in x86_64) DEBARCH=amd64 ;; aarch64) DEBARCH=arm64 ;; *) DEBARCH=$ARCH ;; esac
cat > "$W/common.yaml" <<YAML
name: fasty-browser
version: $VERSION
release: 1
maintainer: Fasty Studio <noreply@users.noreply.github.com>
description: |
  A small, fast, quiet web browser.
  Built-in ad blocker, passkeys, an authenticator, picture in picture and import
  from other browsers. Uses WebKitGTK.
homepage: https://github.com/fasthand-studio/FastyBrowser
license: See LICENSE
contents:
  - src: $R/usr/lib/fasty-browser
    dst: /usr/lib/fasty-browser
    type: tree
  - src: /usr/lib/fasty-browser/fasty-browser
    dst: /usr/bin/fasty-browser
    type: symlink
  - src: $R/usr/share/applications/app.fastybrowser.Linux.desktop
    dst: /usr/share/applications/app.fastybrowser.Linux.desktop
  - src: $R/usr/share/icons/hicolor/scalable/apps/fasty-browser.svg
    dst: /usr/share/icons/hicolor/scalable/apps/fasty-browser.svg
  - src: $W/LICENSE
    dst: /usr/share/doc/fasty-browser/LICENSE
YAML
{ cat "$W/common.yaml"; cat <<YAML
arch: $DEBARCH
section: web
priority: optional
depends:
  - libc6 (>= 2.39)
  - libwebkitgtk-6.0-4
  - libgtk-4-1
  - libadwaita-1-0
  - libsoup-3.0-0
  - libgirepository-1.0-1
  - libgraphene-1.0-0
  - libvulkan1
  - gstreamer1.0-plugins-good
recommends:
  - gstreamer1.0-libav
  - gstreamer1.0-plugins-bad
  - libsecret-tools
YAML
} > "$W/deb.yaml"
{ cat "$W/common.yaml"; cat <<YAML
arch: $ARCH
depends:
  - glibc >= 2.39
  - webkitgtk6.0
  - gtk4
  - libadwaita
  - libsoup3
  - gobject-introspection
  - graphene
  - vulkan-loader
  - gstreamer1-plugins-good
recommends:
  - gstreamer1-plugin-libav
  - gstreamer1-plugins-bad-free
  - libsecret
rpm:
  group: Applications/Internet
YAML
} > "$W/rpm.yaml"
"$NFPM" package -f "$W/deb.yaml" -p deb -t "$OUT/fasty-browser_${VERSION}_${DEBARCH}.deb"
"$NFPM" package -f "$W/rpm.yaml" -p rpm -t "$OUT/fasty-browser-$VERSION-1.$ARCH.rpm"

# AppImage: the program and its helper; WebKitGTK, GTK 4 and libadwaita come from the system
A="$W/Fasty.AppDir"; mkdir -p "$A/usr/lib/fasty-browser" "$A/usr/bin"
cp "$R"/usr/lib/fasty-browser/* "$A/usr/lib/fasty-browser/"
cp "$STAGE/fasty-browser.svg" "$A/fasty-browser.svg"
cp "$STAGE/app.fastybrowser.Linux.desktop" "$A/app.fastybrowser.Linux.desktop"
sed -i 's|^Exec=.*|Exec=fasty-browser %U|' "$A/app.fastybrowser.Linux.desktop"
cat > "$A/AppRun" <<'SH'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
exec "$HERE/usr/lib/fasty-browser/fasty-browser" "$@"
SH
chmod 755 "$A/AppRun"
ARCH=$ARCH "$TOOL" --appimage-extract-and-run "$A" "$OUT/Fasty-$VERSION-$ARCH.AppImage" >/dev/null 2>&1 || \
  ARCH=$ARCH APPIMAGE_EXTRACT_AND_RUN=1 "$TOOL" "$A" "$OUT/Fasty-$VERSION-$ARCH.AppImage"
rm -rf "$W"
(cd "$OUT" && sha256sum fasty-browser-*.tar.gz fasty-browser_*.deb fasty-browser-*.rpm Fasty-*.AppImage > SHA256SUMS)
ls -lh "$OUT"/*.deb "$OUT"/*.rpm "$OUT"/*.AppImage "$OUT"/*.tar.gz | awk '{print "  " $5 "  " $9}'
