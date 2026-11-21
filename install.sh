#!/bin/sh
# Installs Fasty Browser for your user only: no root needed.
#   ./install.sh            install (or update)
#   ./install.sh uninstall  remove the app (your history and settings stay)
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
PREFIX="$HOME/.local"
APPDIR="$PREFIX/lib/fasty-browser"
WK="$PREFIX/opt/webkitgtk6"

if [ "$1" = "uninstall" ]; then
  rm -rf "$APPDIR" "$PREFIX/bin/fasty-browser" \
    "$PREFIX/share/applications/app.fastybrowser.Linux.desktop" \
    "$PREFIX/share/icons/hicolor/scalable/apps/fasty-browser.svg"
  update-desktop-database "$PREFIX/share/applications" 2>/dev/null || true
  echo "Removed. Your data is still in ~/.local/share/fasty-browser"
  exit 0
fi

# trim_webkit drops what Fasty never uses from the private copy: the
# package archive, WebDriver, MiniBrowser and introspection data. The
# headers and .pc files stay, for rebuilding.
trim_webkit() {
  rm -rf "$WK/pkg" "$WK/usr/bin" "$WK/usr/lib/webkitgtk-6.0/MiniBrowser" \
    "$WK/usr/share/gir-1.0" "$WK/usr/lib/girepository-1.0"
}

# WebKitGTK 6.0: the system's if it is there, else a private copy.
if [ ! -e /usr/lib/libwebkitgtk-6.0.so.4 ] && [ ! -e "$WK/usr/lib/libwebkitgtk-6.0.so.4" ]; then
  echo "WebKitGTK 6.0 isn't installed; fetching a private copy (no root needed)..."
  VER=$(pacman -Si webkitgtk-6.0 2>/dev/null | awk '/^Version/{print $3}')
  [ -n "$VER" ] || { echo "Install it with: sudo pacman -S webkitgtk-6.0"; exit 1; }
  MIRROR=$(grep -m1 '^Server' /etc/pacman.d/mirrorlist | sed 's/Server *= *//; s/\$repo/extra/; s/\$arch/x86_64/')
  mkdir -p "$WK/pkg"
  curl -fL# -o "$WK/pkg/wk.pkg.tar.zst" "$MIRROR/webkitgtk-6.0-$VER-x86_64.pkg.tar.zst"
  tar --zstd -xf "$WK/pkg/wk.pkg.tar.zst" -C "$WK" usr
  rm -rf "$WK/pkg"
  # The library looks for its helper processes at a fixed path; point it at
  # a path of the same length that we own.
  LINK="$HOME/.wk6/lib"
  python3 - "$WK/usr/lib" "$LINK" <<'PY'
import glob, sys
lib, link = sys.argv[1], sys.argv[2].encode()
old = b"/usr/lib/webkitgtk-6.0"
if len(link) != len(old):
    sys.exit("home path length mismatch; install webkitgtk-6.0 with pacman instead")
for p in glob.glob(lib + "/libwebkitgtk-6.0.so.4.*"):
    b = open(p, "rb").read()
    open(p, "wb").write(b.replace(old, link))
PY
  trim_webkit
  mkdir -p "$HOME/.wk6"
  ln -sfn "$WK/usr/lib/webkitgtk-6.0" "$LINK"
fi

[ -d "$WK/usr" ] && trim_webkit
command -v go >/dev/null || { echo "Go is needed to build Fasty: sudo pacman -S go"; exit 1; }
"$HERE/build.sh"

mkdir -p "$APPDIR" "$PREFIX/bin" "$PREFIX/share/applications" "$PREFIX/share/icons/hicolor/scalable/apps"
# The first Linux port was Python; this one replaces it.
rm -rf "$APPDIR/search_linux"
install -m 755 "$HERE/fasty-browser-bin" "$APPDIR/fasty-browser-bin"
install -m 755 "$HERE/fasty-browser.sh" "$APPDIR/fasty-browser"
ln -sfn "$APPDIR/fasty-browser" "$PREFIX/bin/fasty-browser"
cp "$HERE/data/fasty-browser.svg" "$PREFIX/share/icons/hicolor/scalable/apps/"
sed "s|^Exec=fasty-browser|Exec=$PREFIX/bin/fasty-browser|" "$HERE/data/app.fastybrowser.Linux.desktop" \
  > "$PREFIX/share/applications/app.fastybrowser.Linux.desktop"
update-desktop-database "$PREFIX/share/applications" 2>/dev/null || true
gtk-update-icon-cache -q "$PREFIX/share/icons/hicolor" 2>/dev/null || true
echo "Installed. Launch Fasty from your app menu, or run: fasty-browser"
