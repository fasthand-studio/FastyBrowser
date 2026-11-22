<p align="center">
  <img src=".github/banner.svg" alt="Fasty Browser" width="100%">
</p>

<p align="center">
  <a href="#install"><img src="https://img.shields.io/badge/platform-Linux-orange?style=flat-square" alt="Linux"></a>
  <a href="#install"><img src="https://img.shields.io/badge/language-Go-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go"></a>
  <a href="#install"><img src="https://img.shields.io/badge/engine-WebKitGTK%206.0-4fc3f7?style=flat-square" alt="WebKitGTK 6.0"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="MIT"></a>
</p>

---

## What is Fasty

Fasty is a Linux browser written in Go. It uses the system's WebKitGTK 6.0
engine with GTK 4 and libadwaita — no bundled Chromium and no Electron.

It also has a built-in agent interface: an AI agent or script can connect over
a Unix socket, read a page, run JavaScript, click elements, and navigate —
without screen scraping or a browser extension.

## Install

```bash
./install.sh            # builds and installs to ~/.local — no root needed
fasty-browser           # launch from the terminal or the app menu
```

Build and runtime tools: Go, Rust (`cargo`), `curl`, `python3`, `pkg-config`,
`objcopy`, and GStreamer plugins for audio and video. On Arch:

```bash
sudo pacman -S go rust curl python3 pkgconf binutils \
  webkitgtk-6.0 gst-plugins-good gst-plugins-bad gst-libav
```

If WebKitGTK 6.0 is missing, `install.sh` downloads and unpacks a private copy
into `~/.local/opt/webkitgtk6` instead of installing it system-wide.

`build.sh` compiles Brave's `adblock-rust` into the browser and bundles
EasyList, EasyPrivacy and StevenBlack's hosts list. The browser refreshes
those lists in the background every three days, recompiles them, and swaps
the filters without a restart (like Brave's component updates). WebKit's own
content blocker enforces them; the "Block ads and trackers" switch and the
per-site pause still work. If a download fails, the last good list is kept.

YouTube can lose its picture while keeping audio after an ad when hardware
video decoding is used on some drivers. Fasty demotes the hardware decoders
(NVDEC, VA-API, VDPAU) so software decoding takes over, while GPU compositing
stays on. Set `GST_PLUGIN_FEATURE_RANK` yourself to override this.

## Features

| | |
|---|---|
| **Tabs** | Side column or top bar, pinned tabs, foldable sidebar, `Ctrl+K` tab switcher, lazy restore, auto-sleep |
| **Blocking** | Ad/tracker blocker from EasyList, EasyPrivacy and StevenBlack hosts, with a toggle and per-site pause |
| **Agent Socket** | JSON commands over `$XDG_RUNTIME_DIR/fasty.sock`: tabs, eval, click, screenshot, and more |
| **Reading** | Reader mode, per-site zoom, clean links, find on page |
| **Hiding** | Pick an element to hide permanently, managed per site |
| **Library** | Searchable bookmarks, history and downloads panels |
| **Media** | Picture-in-picture and remembered camera, microphone and location permissions |
| **Look** | System, light or dark theme with GTK 4 / libadwaita widgets |
| **Extensions** | WebExtension-style add-ons: content scripts, background pages, browser APIs, and an extension store |

## Agent Control

The Unix socket accepts one JSON command per line and replies with one JSON
line.

```bash
# List open tabs
echo '{"action":"tabs"}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/fasty.sock

# Navigate
echo '{"action":"navigate","url":"https://example.com"}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/fasty.sock

# Read page text
echo '{"action":"content"}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/fasty.sock

# Run JavaScript
echo '{"action":"eval","script":"document.title"}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/fasty.sock

# Click an element
echo '{"action":"click","selector":"#login-btn"}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/fasty.sock

# Screenshot
echo '{"action":"screenshot","path":"/tmp/shot.png"}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/fasty.sock
```

<details>
<summary><strong>All actions</strong></summary>

| Action | Fields | Returns |
|---|---|---|
| `tabs` | | array of `{index, url, title, current, loading, pinned, private}` |
| `navigate` | `url` | load in the current tab |
| `open` | `url` | open a new tab |
| `select` | `tab` (1-based) | switch tab |
| `close` | | close the selected tab or popup |
| `back` / `forward` / `reload` | | navigation |
| `url` / `title` | | current page URL or title |
| `eval` | `script` | run JS, return the result |
| `content` | | `document.body.innerText` |
| `html` | | `document.documentElement.outerHTML` |
| `screenshot` | `path` | save PNG, return the path |
| `click` | `selector` | click the first matching element |
| `type` | `text` | resolve text as a URL or search and navigate |
| `wait` | | no-op reply, useful for checking the socket |
| `popups` | | array of popups with `index`, `url`, `title`, `loading`, `built`, `selected` |
| `popup-select` | `tab` (1-based) | select a popup for following commands |
| `state` | | full browser state |
| `extensions` | | list installed extensions |
| `ext-install` | `url` / `path` / `id` | install an extension from a URL, folder or ID |
| `ext-remove` / `ext-toggle` / `ext-reload` | `id`, `enabled` | manage an extension |
| `ext-eval` / `ext-call` / `ext-open` / `ext-action` | `id`, `page`, `message`, `target`, `area` | run or query extension code |
| `ext-logs` / `ext-storage` | `id`, `items` | read extension logs or storage |

</details>

<details>
<summary><strong>Python example</strong></summary>

```python
import os, socket, json

sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(f"{os.environ['XDG_RUNTIME_DIR']}/fasty.sock")
stream = sock.makefile("rw")

def cmd(action, **kw):
    stream.write(json.dumps({"action": action, **kw}) + "\n")
    stream.flush()
    return json.loads(stream.readline())

print(cmd("tabs"))
print(cmd("eval", script="document.title"))
print(cmd("content"))
```

</details>

## Keyboard shortcuts

A few of the common ones: `Ctrl+L` address bar, `Ctrl+K` switch tab,
`Ctrl+T` new tab, `Ctrl+W` close tab, `Ctrl+R` reload, `Ctrl+F` find,
`Ctrl+Shift+R` reader mode, `Ctrl+Shift+H` hide an element, `Ctrl+Y` history,
`Ctrl+Shift+J` downloads, `Ctrl+Shift+E` extensions, `Ctrl+,` settings.
Press `Ctrl+?` in the app for the full list.

## Project structure

```
app.go          application lifecycle, renderer and decoder setup
window.go       window, layout, keyboard shortcuts, find, reader
tab.go          tabs and their lazily built web views
web.go          WebKit sessions, settings, and the content-blocking Shield
adblock.go      Go bridge to adblock-rust
adblock-ffi/    Rust crate that compiles filter lists to WebKit rules
agent.go        JSON-over-Unix-socket control interface
address.go      URL resolution, search engines, link cleaning
models.go       history, bookmarks, hidden elements
store.go        settings and JSON persistence
omnibox.go      address field with inline completion
panels.go       history, bookmarks, downloads and settings panels
tabstrip.go     tab strip (side column and top bar)
downloads.go    download manager
pip.go          picture-in-picture
extension*.go   WebExtension runtime, networking, storage and UI
selftest.go     headless test harness
assets/         embedded CSS, JavaScript and filter lists
```

## Data

```
~/.local/share/fasty-browser/    settings, history, bookmarks, WebKit data
~/.cache/fasty-browser/          cache and compiled content filters
```

## License

MIT — see [LICENSE](LICENSE).

---

<p align="center"><sub>Made by <strong>FastHand Studio</strong></sub></p>
