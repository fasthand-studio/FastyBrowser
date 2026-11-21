#!/bin/sh
# Builds ./fasty-browser-bin. The first build takes a while: the GTK and
# WebKit bindings are big. Later builds take seconds.
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
WK="$HOME/.local/opt/webkitgtk6"
if ! pkg-config --exists webkitgtk-6.0 2>/dev/null; then
  # Build against the private copy: its .pc files, pointed at where it lives.
  mkdir -p "$WK/pc"
  for f in "$WK"/usr/lib/pkgconfig/*.pc; do
    sed -e "s|^prefix=.*|prefix=$WK/usr|" -e "s|^libdir=.*|libdir=$WK/usr/lib|" "$f" > "$WK/pc/$(basename "$f")"
  done
  export PKG_CONFIG_PATH="$WK/pc${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
fi
cd "$HERE"
go build -trimpath -ldflags "-s -w" -o fasty-browser-bin .
echo "Built $HERE/fasty-browser-bin"
