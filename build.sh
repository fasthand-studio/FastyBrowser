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

# Brave's adblock-rust compiles EasyList + EasyPrivacy into WebKit's content
# blocking format. The static library is linked into the Go binary.
LIB="$HERE/adblock-ffi/target/release/libfasty_adblock.a"
if command -v cargo >/dev/null 2>&1; then
  (cd "$HERE/adblock-ffi" && RUSTFLAGS="-Cembed-bitcode=no" cargo build --release --quiet)
  # Go's linker rejects the LLVM bitcode sections rustc leaves in staticlibs.
  if command -v objcopy >/dev/null 2>&1 && [ -f "$LIB" ]; then
    objcopy --remove-section=.llvmbc --remove-section=.llvmcmd "$LIB" 2>/dev/null || true
  fi
elif [ ! -f "$LIB" ]; then
  echo "Rust (cargo) is needed to build the ad blocker: sudo pacman -S rust" >&2
  exit 1
fi

# Bundled filter lists: refreshed weekly, fetched when building. They are
# embedded in the binary by go:embed, so the browser needs no network for them.
mkdir -p "$HERE/src/assets/filters"
for entry in \
  "easylist https://easylist.to/easylist/easylist.txt" \
  "easyprivacy https://easylist.to/easylist/easyprivacy.txt" \
  "stevenblack https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"; do
  set -- $entry
  list="$1"
  url="$2"
  target="$HERE/src/assets/filters/$list.txt"
  if [ ! -s "$target" ] || [ -n "$(find "$target" -mtime +7 -print -quit 2>/dev/null)" ]; then
    echo "Fetching $list filter list..."
    curl -fLsS --max-time 90 -o "$target.tmp" "$url" || \
      { rm -f "$target.tmp"; echo "Could not fetch $list; using the list already on disk if any." >&2; }
    [ -s "$target.tmp" ] && mv "$target.tmp" "$target"
  fi
done

go build -trimpath -ldflags "-s -w" -o fasty-browser-bin ./src
echo "Built $HERE/fasty-browser-bin"
