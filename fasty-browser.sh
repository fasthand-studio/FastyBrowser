#!/bin/sh
# Fasty Browser. Uses the system's WebKitGTK 6.0; if the system has none, the
# private copy that install.sh unpacked into ~/.local/opt/webkitgtk6.
HERE="$(dirname "$(readlink -f "$0")")"
have_webkit() {
  for d in /usr/lib /usr/lib64 /usr/lib/x86_64-linux-gnu /usr/lib/aarch64-linux-gnu /lib64; do
    [ -e "$d/libwebkitgtk-6.0.so.4" ] && return 0
  done
  command -v ldconfig >/dev/null 2>&1 && ldconfig -p 2>/dev/null | grep -q libwebkitgtk-6.0.so.4
}
if ! have_webkit; then
  WK="$HOME/.local/opt/webkitgtk6/usr/lib"
  if [ -e "$WK/libwebkitgtk-6.0.so.4" ]; then
    export LD_LIBRARY_PATH="$WK${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
  else
    echo "fasty: WebKitGTK 6.0 is missing. Install it (Arch: webkitgtk-6.0, Debian/Ubuntu: libwebkitgtk-6.0-4, Fedora: webkitgtk6.0)." >&2
    exit 1
  fi
fi
exec "$HERE/fasty-browser-bin" "$@"
