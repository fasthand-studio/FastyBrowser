<p align="center">
  <img src=".github/banner.svg" alt="Fasty Browser" width="100%">
</p>

<p align="center">
  <a href="#install"><img src="https://img.shields.io/badge/platform-Linux-orange?style=flat-square" alt="Linux"></a>
  <a href="#install"><img src="https://img.shields.io/badge/lang-Go-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go"></a>
  <a href="#install"><img src="https://img.shields.io/badge/engine-WebKitGTK%206.0-4fc3f7?style=flat-square" alt="WebKitGTK"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="MIT"></a>
</p>

---

## What is Fasty

Fasty is a web browser for Linux built from the ground up in **Go**. It runs on the system's own **WebKitGTK 6.0** engine with **GTK 4** and **libadwaita** — no bundled Chromium, no Electron, no wrappers. One ~17 MB binary that feels native and stays out of your way.

It is also the first browser with a **built-in agent interface**. Any AI agent or script can connect to Fasty over a Unix socket, read pages, run JavaScript, click elements, and navigate — no screen scraping, no browser extensions, no middleware.

## Install

```bash
./install.sh            # builds and installs to ~/.local — no root needed
fasty-browser           # launch from terminal or the app menu
```

System dependencies (Arch-based):

```bash
sudo pacman -S webkitgtk-6.0 gst-plugins-good gst-plugins-bad gst-libav rust
```

> `gst-plugins-good` is required — without it, pages with video or audio will crash.
> Rust is needed at build time: `build.sh` compiles Brave's `adblock-rust` into
> the browser and downloads EasyList, EasyPrivacy and StevenBlack's hosts
> (refreshed weekly, bundled into the binary). The blocker uses WebKit's own
> network-level content blocking; the "Block ads and trackers" switch and
> per-site pause still apply.

If a YouTube video plays sound but shows no picture after an ad, Fasty already
demotes hardware video decoders (NVDEC/VA-API/VDPAU) so software decoding takes
over — GPU compositing stays on. Set `GST_PLUGIN_FEATURE_RANK` yourself to
override that.

If WebKitGTK 6.0 is not installed, `install.sh` automatically downloads and unpacks a private copy.

## Features

| | |
|---|---|
| **Tabs** | Side column or top bar, pinned tiles, fold sidebar, switch with `Ctrl+K`, lazy restore, auto-sleep |
| **Privacy** | Built-in ad/tracker blocker in the network stack, per-site toggle, private tabs with isolated cookies |
| **Agent Socket** | AI agents connect via `$XDG_RUNTIME_DIR/fasty.sock` — read pages, eval JS, click, screenshot |
| **Reading** | Reader mode, per-site zoom, clean links, find on page |
| **Hiding** | Pick any element and hide it permanently — manage what's hidden per site |
| **Library** | Searchable bookmarks, history and downloads panels |
| **Media** | Picture-in-picture, camera/mic/location permissions with per-site memory |
| **Look** | Light, dark, or system theme — native GTK 4 integration |

## Agent Control

Fasty opens a Unix socket at startup. Send JSON, get JSON — one command per line.

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
| `navigate` | `url` | load in current tab |
| `open` | `url` | open new tab |
| `select` | `tab` (1-based) | switch tab |
| `close` | | close current tab |
| `back` / `forward` / `reload` | | navigation |
| `eval` | `script` | run JS, return result |
| `content` | | `document.body.innerText` |
| `html` | | `document.documentElement.outerHTML` |
| `url` / `title` | | current page URL or title |
| `screenshot` | `path` | save PNG, return path |
| `click` | `selector` | click first matching element |
| `type` | `text` | address bar input + navigate |
| `state` | | full browser state |

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

## Project Structure

```
app.go          application lifecycle, agent socket
window.go       window, layout, keyboard shortcuts, find, reader
tab.go          tab and its lazily-built web view
web.go          network sessions, web settings, content blocker
agent.go        Unix socket control interface
address.go      URL resolution, search engines, link cleaning
models.go       history, bookmarks, hidden elements
store.go        settings and JSON persistence
omnibox.go      address field with inline completion
panels.go       history, bookmarks, downloads, settings panels
tabstrip.go     tab strip (side column and top bar)
downloads.go    download manager
pip.go          picture-in-picture
selftest.go     headless test harness
assets/         embedded CSS and JS (reader, picker, smooth scroll)
```

## Data

```
~/.local/share/fasty-browser/    settings, history, bookmarks, WebKit data
~/.cache/fasty-browser/          cache, content filters
```

## License

MIT — see [LICENSE](LICENSE).

---

<p align="center"><sub>Made by <strong>FastHand Studio</strong></sub></p>
