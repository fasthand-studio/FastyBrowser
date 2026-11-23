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

- **Fast to start and light on memory**: tabs you aren't using go to sleep,
  and the ad blocker's rules are compiled once and cached.
- **Blocks ads and trackers itself**, before a request is made, with no
  extension and no remote service.
- **Built-in sign-in tools**: passkeys (WebAuthn) and an Authenticator for
  one-time codes, both stored encrypted on your computer.
- **Picture in picture** that plays only the video, with a button on every
  video.
- **An agent interface**: a script or AI agent can connect over a Unix socket,
  read a page, run JavaScript, click elements and navigate, without screen
  scraping or an extension.

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
- Passkeys (`passkeys.json`) and Authenticator accounts (`authenticator.json`)
  are in the same folder, **encrypted** (see below). The keys that open them
  are kept in your system keyring (`secret-tool`/libsecret, such as KDE Wallet
  or GNOME Keyring), or in a file readable only by you if there is no keyring.
- Cookies, cache and WebKit site data live under `~/.local/`, and compiled
  blocker rules in `~/.cache/fasty-browser/`.
- The data and cache folders are readable by your user only (mode 700, files
  600), including data written by older versions, so other accounts on the
  computer can't read your history, open tabs or settings.
- There is no sync service and no cloud backup. Clearing browsing data in
  Settings → Privacy removes local history, cookies, site data and cache. It
  does not touch passkeys or Authenticator accounts: delete those where they
  are listed (Settings → Privacy).

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
- **Other apps** (mail, Steam, Zoom, an editor, and so on) are only opened
  from a link you click or a key you press. A page that tries to launch one on
  its own, by redirecting or from a script, is stopped.
- The **agent socket** is a local Unix socket at
  `$XDG_RUNTIME_DIR/fasty.sock`, created for your user only. It is off by default: Settings → Privacy →
  Agent control turns it on or off (at once, and turning it off ends open
  connections). `FASTY_AGENT=1 fasty-browser` starts it for one run. Any process
  running as you can connect to it and control open tabs, so keep that in
  mind when granting local access.

## Install

```bash
./install.sh            # builds and installs to ~/.local — no root needed
fasty-browser           # launch from the terminal or the app menu
```

Build and runtime tools: Go (modules are downloaded at build time), Rust
(`cargo`), `curl`, `python3`, `pkg-config`, `objcopy`, and GStreamer plugins
for audio and video. `libsecret` (`secret-tool`) is optional: without it the
Authenticator and passkey keys are kept in a private file instead of the
keyring. On Arch:

```bash
sudo pacman -S go rust curl python3 pkgconf binutils \
  webkitgtk-6.0 gst-plugins-good gst-plugins-bad gst-libav libsecret
```

If WebKitGTK 6.0 is missing, `install.sh` downloads and unpacks a private copy
into `~/.local/opt/webkitgtk6` instead of installing it system-wide.

`build.sh` compiles the ad blocker into the browser; see **How blocking works**
below for what it does.

YouTube can lose its picture while keeping audio after an ad when hardware
video decoding is used on some drivers. Fasty demotes the hardware decoders
(NVDEC, VA-API, VDPAU) so software decoding takes over, while GPU compositing
stays on. Set `GST_PLUGIN_FEATURE_RANK` yourself to override this.

GPU decoding is an option, off by default: see **GPU video decoding** below.

## How blocking works

The blocker is built in — no extension, no proxy, and no remote filtering
service. It uses Brave's **adblock-rust** engine to turn three public lists —
**EasyList**, **EasyPrivacy** and **StevenBlack's hosts** — into WebKit's own
content-blocking rules. WebKit enforces them before a request leaves the
browser, so blocked ad and tracker requests are never made.

- Rules are split into two WebKit filters because WebKit allows only about
  150,000 rules per filter. WebKit caches the compiled rules on disk, under a
  name made from the lists themselves, so the rules are only rebuilt (a few
  hundred MB of work) when a list changes, not on every launch.
- Every three days the browser downloads fresh lists from `easylist.to` and
  GitHub, recompiles them with adblock-rust and swaps the filters in without a
  restart. It checks again daily. If a download fails, the last good list is
  kept; if no list is available at all, a small built-in list of known ad
  domains is used.
- The **Block ads and trackers** switch in Settings → Privacy turns the
  blocker on and off globally, and **About this site** can pause it for a
  single host.

### YouTube

YouTube's own ads are part of the page itself and have no separate URL to
block, so `assets/js/youtube.js` works the way Brave's and uBlock Origin's
YouTube scriptlets do:

- the inline player response never carries ad keys;
- `JSON.parse` and `Response.json` results are pruned;
- fetch and XHR responses for player, get_watch, next, browse, playlist and
  Shorts requests have `adPlacements`, `adSlots` and `playerAds` renamed to
  `no_ads`, and ad entries are dropped from lists;
- ads that are baked into the stream (YouTube's server-side-inserted ads) are
  seeked past as soon as the player reports one;
- the "ad blockers violate the terms" popup is dismissed;
- ad containers are hidden with Brave's and EasyList's exact selectors.

YouTube changes its ads often, so some will slip through; this is a best
effort, not a guarantee.

## Features

| | |
|---|---|
| **Tabs** | Side column or top bar, pinned tabs, foldable sidebar, `Ctrl+K` tab switcher, lazy restore, auto-sleep |
| **Blocking** | Ad/tracker blocker from EasyList, EasyPrivacy and StevenBlack hosts, YouTube ad handling, a toggle and per-site pause |
| **Agent Socket** | JSON commands over `$XDG_RUNTIME_DIR/fasty.sock`: tabs, eval, click, screenshot, and more |
| **Reading** | Reader mode, per-site zoom, clean links, find on page |
| **Hiding** | Pick an element to hide permanently, managed per site |
| **Library** | Searchable bookmarks, history and downloads panels |
| **Media** | Picture in picture, and remembered camera, microphone and location permissions |
| **Sign-in** | Passkeys (WebAuthn) and an Authenticator for one-time codes, kept encrypted on this computer |
| **Look** | System, light or dark theme with GTK 4 / libadwaita widgets |
| **Extensions** | WebExtension-style add-ons: content scripts, background pages, browser APIs, and an extension store |

## Picture in picture

WebKitGTK has no picture-in-picture of its own, so Fasty provides it, the way
Firefox does: the video stays in its tab, playing in its own page, and its
picture is copied out into a small window. No second copy of the page is
loaded.

- Point at any video and a small PiP button appears in its corner. Click it, or
  press `Ctrl+Shift+P`, to pop the video out into a small window that stays on
  top (on KDE; elsewhere use your window manager's "always on top").
- The tab keeps its page, and its video area says "This video is playing in
  Picture-in-Picture". The sound keeps coming from the tab. Click the video
  area to close the pop-up.
- The pop-up has a title bar that appears when you move the mouse over it, with
  full screen, "Back to Tab" and close, and play/pause, a seek bar and the
  time along the bottom. These act on the page's own video. Space or `K`
  plays and pauses, the arrow keys skip 5 seconds, `F` or a double click is
  full screen.
- It works while another tab is in front: the page of a tab whose video is
  being copied is kept on screen (unseen) so WebKit doesn't throttle it.
- How it works: `assets/js/pipcapture.js` draws each frame small (about 30 a
  second, sharper if you make the window bigger) onto a canvas, encodes it as a
  JPEG and sends it to the browser, which shows it in the window
  (`pip_capture.go`). It costs some CPU in the page while it runs: about a
  quarter of a core more (54% against 31% of a core for the same YouTube
  video without it, in testing). Video decoding itself is on the CPU by
  design (see Install), so the GPU isn't doing that work.
- Videos whose pixels the page doesn't let scripts read (some cross-origin
  files, protected video) can't be copied. A plain video file then plays in a
  small video-only view (`pip_mirror.go`, which uses a second view and some
  extra memory); anything else moves the tab's whole page into the pop-up with
  only the video showing.
- If the tab goes to another page, the pop-up closes.

## Passkeys

WebKitGTK has no WebAuthn, so sites hid their passkey and "Continue with
Google" options. Fasty defines the standard API (`navigator.credentials`,
`PublicKeyCredential`) and answers it with a built-in authenticator:

- Passkeys are made and used only for the site in the tab's own address, after
  you confirm each time. Nothing happens silently.
- Private keys are encrypted (AES-256-GCM) and the key is in your system
  keyring. Settings → Privacy → Passkeys lists them and can delete them.
- Limits: ES256 only, no attestation, no autofill-style prompts, no USB
  security keys. Passkeys stay on this computer: they don't sync, and
  passkeys from your phone or other browsers are not available here. The
  confirmation dialog counts as the "user verification" some sites ask for,
  which is weaker than a fingerprint or PIN.
- `webauthn.go`, `webauthn_store.go`, `webauthn_cbor.go` and
  `assets/js/webauthn.js` hold the code.

## Authenticator

Settings → Privacy → Authenticator (or `Ctrl+Shift+A`) opens a window of
one-time codes, like Proton Authenticator or Google Authenticator:

- Add accounts with a secret key or an `otpauth://` link. Each shows its
  current code, a ring counting down to the next one, and the next code. Click
  a code to copy it. SHA-1, SHA-256 and SHA-512, 6 or 8 digits, any period
  (RFC 6238; checked against its test vectors).
- Accounts are stored encrypted with a key kept in the system keyring.
- **PIN** (off until you turn it on, Settings → Privacy → Authenticator →
  Require a PIN): the key is taken out of the keyring and kept only in a form
  that needs the PIN (PBKDF2-HMAC-SHA256, 600,000 rounds), so the PIN protects
  the codes even from someone with your files and login. Three wrong tries
  start a wait that doubles each time. A forgotten PIN can't be recovered.
- **Toolbar button** (off until you turn it on, "Show in the toolbar"): a key
  icon next to the extensions button; right-click it to unpin.
- **Import** reads text files of `otpauth://` links, Proton Authenticator
  backups (including its password-locked ones), Aegis and 2FAS unencrypted
  exports, Bitwarden JSON, Google Authenticator's "export accounts" link, and
  Fasty's own locked files. Accounts already present are skipped.
- **Export** writes a text file of `otpauth://` links (unprotected: anyone with
  the file can use your codes) or the same locked with a password
  (AES-256-GCM, key from PBKDF2).
- Limits: time-based codes only (no counter-based HOTP, no Steam codes), no QR
  scanning, encrypted Aegis and 2FAS backups must be exported unencrypted
  first.

## GPU video decoding

Settings → General → "GPU video decoding (NVIDIA, experimental)", off by
default. Video is decoded on the CPU unless you turn it on.

Why it isn't just on: WebKit decodes video inside its sandboxed web process,
and NVIDIA's decoder (NVDEC) can't start there, because it needs
`/dev/nvidia-uvm`, which WebKit's sandbox leaves out. Firefox has the same
trouble on NVIDIA (its users must switch a sandbox off). Fasty solves it with
open-source pieces and one small helper:

- **gst-plugin-va** and **libva-nvidia-driver** (install both) decode through
  NVDEC. GStreamer's va plugin ignores that driver unless `GST_VA_ALL_DRIVERS`
  is set, and it caches what it found, so Fasty keeps its own GStreamer
  registry, made once with that set.
- `native/bwrap_uvm.c` is preloaded into Fasty (`install.sh` builds it when
  `gcc` is present). WebKit starts the web process with
  `bwrap --args FD -- WebKitWebProcess`; the helper adds the two UVM device
  nodes to that sandbox's arguments, and nothing else. The sandbox stays on.
- `gpudecode.go` checks what's there, sets the variables, builds the registry
  the first time (a few seconds) and restarts Fasty once with the helper
  loaded. Settings shows what is missing if the switch is greyed out.

Measured on an RTX 3060 with 1080p YouTube: the web process used about 32% of
a core with it on and about 45% with it off, and `nvidia-smi` showed the
decoder busy (5–8%). Limits: NVIDIA only (Intel and AMD cards decode on the GPU
without this when gst-plugin-va is installed), it widens the web process's
access by two device nodes, and it relies on how WebKitGTK starts its sandbox,
which could change in a future version, in which case the option quietly stops
having an effect.

## Memory and speed

On one test machine (Arch, NVIDIA RTX 3060, seven pinned tabs, one page
loaded) the browser used about 470 MB in total: the window about 165 MB, a
typical page about 260 MB and the network process about 50 MB. A heavy page
such as YouTube takes 600 MB or more. What keeps it down:

- Tabs you haven't used for 30 minutes (Settings → General) go to sleep and
  give their memory back; they also sleep sooner when the system is short of
  memory.
- A page process starts dropping caches at about 150 MB and everything it can at
  about 300 MB (WebKit's own defaults wait until about 500 and 900 MB). On seven
  logged-out sites this took the total from 2.5 to 3.1 GB down to about 2.1 GB,
  for a few percent more idle CPU.
- The back/forward page cache is off, so a page you leave is let go instead of
  being kept alive for Back. Back reloads from the HTTP cache.
- The blocker's compiled rules are cached by content, so startup doesn't
  rebuild them.

Keep **Hardware acceleration** on: turning it off cost about 28% of the
Speedometer 3.1 score in testing. Fasty also avoids work that slows pages: for
example `assets/js/compat.js` stops GitHub's tab bars from running a
scroll-driven animation that halved the frame rate while scrolling.

## Site compatibility

`assets/js/compat.js` holds small fixes for sites that expect things WebKitGTK
lacks. Today it makes GitHub load its sign-in options (Google, Apple) and
stops GitHub's tab bars from slowing scrolling. Fixes are limited to the one
site they are for.

## Installing from a release

`packaging/make-release.sh` builds `dist/fasty-browser-VERSION-linux-ARCH.tar.gz`
and a `SHA256SUMS` file. Unpack it and run `./install.sh`.

With a screen, a window opens (GTK 4 and libadwaita). Without one, or with
`--cli`, the same questions come up in the terminal. It installs for your user
only; the one thing that asks for a password is installing missing packages,
and it does all of them in one go.

1. **What Fasty needs.** It checks the libraries and the video plugins. One
   button installs whatever is missing for your system: Arch (and CachyOS,
   Manjaro, EndeavourOS), Debian and Ubuntu (and Mint, Pop!_OS), Fedora (and
   its relatives). On Fedora that includes RPM Fusion's H.264 codec, which
   Fedora leaves out of its own packages.
2. **Search engine.**
3. **Default browser.**
4. **GPU video decoding**, only if you have an NVIDIA card.
5. **Bring your things over** from Firefox, Zen, LibreWolf, Waterfox, Chrome,
   Chromium, Brave, Vivaldi, Edge or Opera, or from a bookmarks file.
   Passwords and cookies are not imported.

For scripts: `./install.sh --yes --default-browser no --engine duckduckgo
--import none`. `./install.sh --uninstall` removes the program and keeps your data.

`packaging/make-packages.sh` then builds a `.deb` (Debian, Ubuntu), an `.rpm`
(Fedora) and an AppImage from the same build. The packages pull in WebKitGTK, GTK 4
and libadwaita through the system's package manager. The AppImage carries only
Fasty: those libraries must already be on the system.

**Which systems.** The program is built on Arch against glibc 2.39, so it needs
a system that new or newer: Ubuntu 24.04+, Debian 13+, Fedora 40+, current Arch.
The installer says so, and refuses by default, on anything older.

## Agent Control

The socket is off by default. Turn it on in Settings → Privacy → Agent control,
or start Fasty with `FASTY_AGENT=1 fasty-browser`.

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
| `pip-eval` | `script` | run JS in the current tab's video-only Picture-in-Picture window |

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
`Ctrl+Shift+J` downloads, `Ctrl+Shift+E` extensions, `Ctrl+Shift+A`
Authenticator, `Ctrl+Shift+P` Picture in picture, `Ctrl+,` settings.
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

gpudecode.go          the GPU video decoding option (see above)
native/bwrap_uvm.c    the helper it preloads (C), built by install.sh
pip.go                picture in picture: choosing a way, closing, the old whole-page window
pip_capture.go        the pop-up that shows frames copied from the page
pip_mirror.go         the fallback video-only view for plain video files
pip_scripts.go        the scripts the fallbacks run inside pages
pip_kwin.go           keeping the pop-up on top on KDE
webauthn.go           passkeys: requests, asking, making and signing
webauthn_store.go     passkeys saved on disk, encrypted
webauthn_cbor.go      the CBOR a passkey needs

authenticator.go      one-time codes (TOTP), the encrypted vault and the optional PIN
authenticator_ui.go   the Authenticator window, import/export and its Settings rows
authenticator_io.go   import and export: text, locked files, Proton, Aegis, 2FAS, Google, Bitwarden
authenticator_test.go RFC 6238 test vectors and vault/PIN tests
authenticator_io_test.go import/export tests

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
assets/style.css      the stylesheet
assets/js/            scripts the browser runs inside pages:
                        picker.js (element picker), wheel.js (smooth wheel),
                        youtube.js (YouTube ad data), pipbutton.js (the
                        Picture-in-Picture button), pipcapture.js (copying
                        the video out), webauthn.js (passkeys),
                        compat.js (site fixes), reader.js, webext.js and
                        ext_store.js (extensions)
assets/filters/       bundled EasyList, EasyPrivacy and hosts lists
```

## Testing

```bash
go test .                # codes (RFC 6238 vectors), the encrypted vault and PIN,
                         # import and export, including a Proton backup
```

`FASTY_SELFTEST=script fasty-browser` drives a separate instance through a
script (open pages, type, press keys, take screenshots); the verbs are listed
at the top of `selftest.go`. The agent socket (above) is the other way to test
a running browser.

## Data

```
~/.local/share/fasty-browser/    settings, history, bookmarks, hidden
                               elements, permissions, session, extensions,
                               passkeys.json and authenticator.json (both
                               encrypted), cookies and WebKit site data
~/.cache/fasty-browser/          cache, filter lists and compiled rules
system keyring                   the keys for passkeys and the Authenticator
                               (a 0600 file next to the data if none)
```

All of it is local and is never uploaded.

## License

MIT — see [LICENSE](LICENSE). Settings → Open Source lists the projects Fasty
is built on and their licenses.

---

<p align="center"><sub>Made by <strong>FastHand Studio</strong></sub></p>
