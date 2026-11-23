#!/bin/sh
# Builds a release: dist/fasty-browser-VERSION-linux-ARCH.tar.gz (and SHA256SUMS).
# The tarball holds the program, its helper library and an installer
# (install.sh) that asks the first-run questions. Needs Go, Rust, gcc and python3.
set -e
HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$HERE"
VERSION=$(sed -n 's/^[[:space:]]*version *= *"\([0-9][0-9.]*\).*/\1/p' store.go | head -1)
[ -n "$VERSION" ] || { echo "couldn't read the version from store.go" >&2; exit 1; }
ARCH=$(uname -m)
NAME="fasty-browser-$VERSION-linux-$ARCH"
OUT="$HERE/dist"
STAGE="$OUT/$NAME"

echo "Testing the installer ..."
python3 -W ignore -m unittest discover -s packaging -p test_install.py
echo "Building $NAME ..."
./build.sh
rm -rf "$STAGE" "$OUT/$NAME.tar.gz"
mkdir -p "$STAGE"

install -m 755 fasty-browser-bin "$STAGE/fasty-browser-bin"
command -v strip >/dev/null 2>&1 && strip --strip-unneeded "$STAGE/fasty-browser-bin" 2>/dev/null || true
install -m 755 fasty-browser.sh "$STAGE/fasty-browser"
if command -v gcc >/dev/null 2>&1; then
  gcc -shared -fPIC -O2 -o "$STAGE/libfasty-uvm.so" native/bwrap_uvm.c -ldl
else
  echo "(gcc isn't here, so the GPU decoding helper is left out)"
fi
cp data/fasty-browser.svg data/app.fastybrowser.Linux.desktop LICENSE "$STAGE/"
install -m 755 packaging/install.py "$STAGE/install.py"
cat > "$STAGE/install.sh" <<'SH'
#!/bin/sh
# Installs Fasty Browser for your user and asks a few first-run questions.
command -v python3 >/dev/null 2>&1 || { echo "python3 is needed to run the installer." >&2; exit 1; }
exec python3 "$(dirname "$(readlink -f "$0")")/install.py" "$@"
SH
chmod 755 "$STAGE/install.sh"
cat > "$STAGE/README.txt" <<EOT
Fasty Browser $VERSION

Install:   ./install.sh
It installs for your user only (no root), then asks: whether to make Fasty your
default browser, which search engine to use, whether the libraries it needs are
there, and whether to bring bookmarks and history over from another browser.

Without questions (for scripts):
  ./install.sh --yes --default-browser no --engine duckduckgo --import none
  ./install.sh --help

Fasty needs WebKitGTK 6.0, GTK 4, libadwaita and the GStreamer plugins. The
installer checks for them and tells you the package names for your system.

Remove:    rm -rf ~/.local/lib/fasty-browser ~/.local/bin/fasty-browser \\
              ~/.local/share/applications/app.fastybrowser.Linux.desktop
Your data (history, bookmarks, settings) is in ~/.local/share/fasty-browser.
Source and notes: https://github.com/fasthand-studio/FastyBrowser
EOT

tar --sort=name --owner=0 --group=0 --numeric-owner -C "$OUT" -czf "$OUT/$NAME.tar.gz" "$NAME"
(cd "$OUT" && sha256sum "$NAME.tar.gz" > SHA256SUMS)
echo "Built:"
ls -lh "$OUT/$NAME.tar.gz" | awk '{print "  " $5 "  " $9}'
cat "$OUT/SHA256SUMS" | sed 's/^/  /'
