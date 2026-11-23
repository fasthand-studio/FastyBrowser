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

## Privacy

**Fasty does not collect any data.** There is no account, telemetry, crash
reporting, analytics or remote configuration. Nothing is sent to FastHand
Studio or any other server by the browser itself. The only network requests
Fasty makes on its own are the public filter-list downloads described below;
everything else is a page you chose to open (including your chosen search
engine and any extension you installed).

Your data stays on this machine:

- History, bookmarks, settings, permissions, hidden elements and the open-tab
  session are JSON files in `~/.local/share/fasty-browser/`.
- Cookies, cache and WebKit site data live under `~/.local/`, and compiled
  blocker rules in `~/.cache/fasty-browser/`.
- There is no sync service and no cloud backup. Clearing browsing data in
  Settings → Privacy removes local history, cookies, site data and cache.

How the privacy features work:

- **Tracking prevention** (on by default) enables WebKit's Intelligent
  Tracking Prevention and rejects third-party cookies. Turning it off accepts
  all cookies.
- **Private tabs** use a separate, in-memory WebKit network session: no
  persistent cookies, credentials or site data are written for those tabs.
- **Clean links** strips common tracking parameters (`utm_*`, `fbclid`,
  `gclid`, YouTube's `si`, and others) before navigating.
- **Permissions** (camera, microphone, location, notifications, and more) are
  remembered per site in the local `settings.json` file.
- **Per-site blocker pause** turns blocking off for one host without changing
  the global switch.
- The **agent socket** is a local Unix socket at
  `$XDG_RUNTIME_DIR/fasty.sock`, created for your user only. Any process
  running as you can connect to it and control open tabs, so keep that in
  mind when granting local access.

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

`build.sh` compiles the ad blocker into the browser; see **How blocking works**
below for what it does.

YouTube can lose its picture while keeping audio after an ad when hardware
video decoding is used on some drivers. Fasty demotes the hardware decoders
(NVDEC, VA-API, VDPAU) so software decoding takes over, while GPU compositing
stays on. Set `GST_PLUGIN_FEATURE_RANK` yourself to override this.

## How blocking works

The blocker is built in — no extension, no proxy, and no remote filtering
service. It uses Brave's **adblock-rust** engine to turn three public lists —
**EasyList**, **EasyPrivacy** and **StevenBlack's hosts** — into WebKit's own
content-blocking rules. WebKit enforces them before a request leaves the
browser, so blocked ad and tracker requests are never made.

- Rules are split into two WebKit filters because WebKit allows only about
  150,000 rules per filter. WebKit caches the compiled rules on disk.
- Unlike third-party ads, YouTube's own ads are part of the page itself and
  have no separate URL to block. A cosmetic rule hides in-feed ad cards, and
  a main-world script strips `adPlacements`, `adSlots` and `playerAds` from
  the inline player response and from player/watch/playlist responses before
  the player sees them — the same technique uBlock Origin and AdGuard use.
  No ad is scheduled, so no ad video is requested.
- Every three days the browser downloads fresh lists from `easylist.to` and
  GitHub, recompiles them with adblock-rust and swaps the filters in without a
  restart. It checks again daily. If a download fails, the last good list is
  kept; if no list is available at all, a small built-in list of known ad
  domains is used.
- The **Block ads and trackers** switch in Settings → Privacy turns the
  blocker on and off globally, and **About this site** can pause it for a
  single host.

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
| **Sign-in** | An Authenticator for one-time codes (Settings > Privacy), with an optional PIN, and passkeys (WebAuthn) kept encrypted on this computer |
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

Everything is one Go package (`package main`), split into small files by job.
Related files share a prefix.

```
app.go                application lifecycle, renderer and decoder setup
store.go              settings and JSON persistence
address.go            URL resolution, search engines, link cleaning
history.go            history and address suggestions
bookmarks.go          bookmarks
curtain.go            hidden page elements
downloads.go          download manager
selftest.go           headless test harness

window.go             the window and its layout
window_tabs.go        opening, closing, moving, pinning and sleeping tabs
window_nav.go         back, forward, reload, zoom, full screen, reader mode
window_find.go        find in page
window_veil.go        the element picker and hidden-element list
window_data.go        bookmarks, downloads, site data, clipboard
window_session.go     saving and restoring tabs
window_keys.go        keyboard shortcuts
tab.go                a tab and its lazily built web view
tab_events.go         navigation, new windows, crashes, menus, page messages
tabstrip.go           the tab strip (side column and top bar)
omnibox.go            the address field with inline completion
spotlight.go          the quick-open box
panels.go             history, bookmarks, downloads and tab-switcher panels
panels_site.go        the site card and permission questions
panels_settings.go    the Settings window

web.go                WebKit sessions, settings, content managers (cgo)
adblock.go            Go bridge to adblock-rust (cgo)
adblock-ffi/          Rust crate that compiles filter lists to WebKit rules
shield_rules.go       filter lists and the rules built from them
shield.go             compiling rules into WebKit filters and refreshing them

pip.go                picture in picture: the window and its controls
pip_mirror.go         the video-only pop-up
pip_scripts.go        the scripts it runs inside pages
pip_kwin.go           keeping the pop-up on top on KDE
webauthn.go           passkeys: requests, asking, making and signing
webauthn_store.go     passkeys saved on disk, encrypted
webauthn_cbor.go      the CBOR a passkey needs

authenticator.go      one-time codes (TOTP), the encrypted vault and the optional PIN
authenticator_ui.go   the Authenticator window and its Settings rows
authenticator_test.go RFC 6238 test vectors and vault/PIN tests

agent.go              JSON-over-Unix-socket control interface
agent_ext.go          its extension commands

extensions.go         extension manifests and the extension manager
extinstall.go         installing, removing and enabling extensions
extmatch.go           match patterns
extruntime.go         the message router between pages and extensions
extinject.go          putting extension scripts and styles into pages
extbackground.go      background pages
extlocale.go          extension text, icons and badges
extweb.go             store pages and pages that talk to extensions
extcontrol.go         driving extensions from the browser and agent
extapi_storage.go     chrome.storage
extapi_tabs.go        chrome.tabs and chrome.scripting
extapi_dnr.go         chrome.declarativeNetRequest (see extdnr*.go)
extapi_extra.go       chrome.management, bookmarks, history, cookies, downloads and more
extdyn.go             run-time content scripts
extmenus.go           chrome.contextMenus
extcapture.go         offscreen documents and tab capture
extcommands.go        extension keyboard shortcuts
extwindows.go         extension windows and sign-in flows
extdnr.go             declarativeNetRequest state and rebuilding
extdnr_convert.go     DNR rules to WebKit filters
extnet.go             extension networking
extnative.go          native messaging
extscheme.go          the extension URL scheme (cgo)
extension_ui.go       extension toolbar buttons, popups and panels
assets/               embedded CSS, JavaScript and filter lists
```

## Data

```
~/.local/share/fasty-browser/    settings, history, bookmarks, hidden
                               elements, permissions, session, extensions,
                               cookies and WebKit site data
~/.cache/fasty-browser/          cache, filter lists and compiled rules
```

All of it is local and is never uploaded.

## License

MIT — see [LICENSE](LICENSE).

---

<p align="center"><sub>Made by <strong>FastHand Studio</strong></sub></p>
