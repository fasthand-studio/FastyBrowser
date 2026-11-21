#!/bin/sh
# Fasty Browser. Uses the system's WebKitGTK 6.0 when it is installed
# (sudo pacman -S webkitgtk-6.0); otherwise the private copy that
# install.sh unpacked into ~/.local/opt/webkitgtk6.
HERE="$(dirname "$(readlink -f "$0")")"
if [ ! -e /usr/lib/libwebkitgtk-6.0.so.4 ]; then
  WK="$HOME/.local/opt/webkitgtk6/usr/lib"
  if [ -e "$WK/libwebkitgtk-6.0.so.4" ]; then
    export LD_LIBRARY_PATH="$WK${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
  else
    echo "fasty: WebKitGTK 6.0 is missing. Install it with: sudo pacman -S webkitgtk-6.0" >&2
    exit 1
  fi
fi
exec "$HERE/fasty-browser-bin" "$@"
