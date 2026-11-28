# Fasty for Windows

The Windows build lives in `windows/`. It is a separate Go module (`fastybrowser-windows`), so the Linux
build in `src/` is not touched by it and does not need it. Current version: **1.2.0**.

## What it is

Linux Fasty is Go on GTK 4 and WebKitGTK. WebKitGTK does not run on Windows, so the Windows build uses
**Microsoft Edge WebView2** (the Chromium-based web control that ships with Windows 11 and current
Windows 10) to draw pages, and draws its own window around it in the same design as Linux. It is pure Go
(no cgo, no C compiler, no Windows SDK) plus a small TypeScript bundle for the voice screen.

Pages are drawn by Chromium here, so the Linux statement "no Chromium" does not apply to Windows.

## Requirements

- Windows 10 or Windows 11, 64-bit (x64). WebView2 supports both; **only Windows 11 has been tested**.
- The Microsoft Edge WebView2 Runtime (already on Windows 11 and up-to-date Windows 10):
  <https://go.microsoft.com/fwlink/p/?LinkId=2124703>.
- For voice search: a microphone and the speech recognizer that ships with Windows (English US is
  installed by default; others via Settings, Time & language, Speech).
- No administrator rights are needed; the installer installs for the current user.

## Same as Linux

| Linux feature | Windows |
| --- | --- |
| Tabs down the left, or across the top (`Ctrl+Shift+S`) | Yes, same layout, colours, sizes and light/dark palette |
| Resizable sidebar, fold it away (`Ctrl+S`) | Yes (drag the edge; hover peek is not done) |
| Pinned tabs as tiles, drag to reorder | Yes |
| Address field with suggestions from tabs, bookmarks, history | Yes |
| Spotlight new tab, `Ctrl+K` tab switcher | Yes |
| Private tabs (`Ctrl+Shift+N`) | Yes: each is its own profile, deleted when the tab closes |
| Reopen closed tab, duplicate tab, jump to tab `Ctrl+1` to `9` | Yes |
| Bookmarks, history | Yes (own pages, search, remove) |
| Sleeping tabs (5 minutes by default) | Yes |
| Session restore | Yes (pinned tabs included) |
| Shield (ad and tracker blocking) | Yes, with host lists (see below) |
| Clean links (strip `utm_*`, `fbclid` ...) | Yes |
| Reading mode (`Ctrl+Shift+R`) | Yes, the Linux `reader.js` |
| Authenticator, one-time codes (`Ctrl+Shift+A`) | Yes: TOTP, SHA-1/256/512, 6 to 8 digits; stored encrypted with Windows DPAPI |
| Find, print, zoom, inspector | Yes, through WebView2 (`Ctrl+F`, `Ctrl+P`, `Ctrl++`, `F12`) |
| Settings | Yes: engine, appearance, tab layout, sidebar, Shield, clean links, restore, sleep time |
| Keyboard shortcuts | Same keys (see the Keyboard Shortcuts page in the app) |
| Passkeys | Provided by WebView2 and Windows Hello; not checked here |

## New on Windows

**Voice search.** The microphone button in the address field, the new-tab box and the menu opens a voice
screen built on the Dictation Dune component: your voice level is a mound of glyphs at the bottom of the
box and each recognised word flies out of it into the text. Stop talking (or press stop) and it searches.
The words come from the speech recognizer that ships with Windows, running on the PC: no audio and no text
leave the computer (`windows/voice/voice.ps1`). Chromium's own web speech service does not work inside
WebView2 (it answers "network"), which is why the Windows recognizer is used. Accuracy is that of the
Windows recognizer, which is good for short phrases and weaker than cloud services.

## Not done yet (compared with Linux)

Extensions, picture in picture, the agent socket, hiding page elements ("Hide Something"), GPU decode
setting, the adblock-rust engine (Shield uses host lists, which block less than EasyList rules), a
downloads panel (Downloads opens the folder; WebView2 shows its own download prompt), the Authenticator's
PIN, import and export, folded-sidebar hover peek, auto-update, ARM64, and code signing.

## Tested

`fasty.exe --selftest` starts the real app against a local web server and checks 24 things: page loading
and titles, Shield blocking, `window.open`, address input, back and forward, `javascript:` input being
searched, the Spotlight new tab, suggestions, bookmarks, history, the Authenticator (code and encrypted
file), pinning, private tabs, reopen closed tab, the settings page and the top layout, folding the sidebar,
reading mode, the voice screen receiving words, full screen, sleeping and waking tabs, session saving, and
closing every tab. All pass on Windows 11 (build 26300).

Unit tests cover address handling, settings, Shield, bookmarks, history, suggestions, and TOTP against the
RFC 6238 test vectors. An integration test has Windows synthesize a sentence and checks that the voice
helper recognises it ("Search for cheap flights to London", with live interim words first).

The installer was installed, run (self-test from the installed copy) and uninstalled with nothing left behind.

**Not tested:** Windows 10, speaking into a microphone by a person (the test uses synthesized speech),
other languages, and any Windows with the WebView2 Runtime missing (the installer warns in that case).

## Build

Needs Go 1.27 or newer. Node is only needed to rebuild the voice bundle (`windows/ui-src`, the built
`windows/ui/voice.js` is committed). Inno Setup 6 is only needed for the installer
(`winget install JRSoftware.InnoSetup`).

```powershell
cd windows
.\build.ps1                      # vet, test, build dist\fasty.exe
.\build.ps1 -Installer -Zip      # also the installer and the portable zip
.\build.ps1 -SelfTest            # also run the in-app self-test (opens a window for about 40 s)
```

Output: `windows/dist/fasty.exe` (about 8 MB), `windows/dist/installer/Fasty-Setup-<version>-x64.exe`
(about 4.5 MB) and `windows/dist/Fasty-<version>-x64-portable.zip`. CI workflows are in `windows/ci/`
until they can be copied to `.github/workflows/` (see its README).

To rebuild the voice bundle: `cd windows/ui-src && npm install && npm run build`.

Measure memory and idle CPU with `windows\tools\bench.ps1 -SleepMinutes 0` and `-SleepMinutes 5`.

## Data

- Settings, session, bookmarks, history, Authenticator accounts (encrypted): `%AppData%\FastyBrowser`.
- Web data (cookies, cache): `%LocalAppData%\FastyBrowser\WebView2`.
- Log: `%LocalAppData%\FastyBrowser\fasty.log`.

## Performance

Chromium flags `--disable-background-networking --no-pings --disable-breakpad` cut idle background work, and
sleeping tabs free the page of tabs you are not using. Measured on the development PC with five real
sites open (one run each, so treat as indicative; page weight varies between runs):

| Setting | Working set | Private memory | Idle CPU (all cores) |
| --- | --- | --- | --- |
| Sleeping tabs off | 1109 MB | 641 MB | 0.24 % |
| Sleeping tabs on (default) | 613 MB | 336 MB | 0.10 % |

Helium (<https://github.com/imputnet/helium>) patches the Chromium source tree. Fasty on Windows does not
build Chromium, so those patches do not apply; only runtime flags and tab sleeping are used here.

## Security notes

- The browser's own pages are served from a loopback-only server with a random path token, and the app
  accepts privileged messages (settings, bookmarks, voice, Authenticator) only from that origin. Web pages
  can only report their title, address and shortcuts.
- Only the browser's own pages get the microphone, without a prompt. Websites keep WebView2's normal prompt.
- Authenticator accounts are encrypted with DPAPI (tied to your Windows account). The file is useless on
  another account or PC.
- `javascript:`, `data:` and `file:` text typed in the address field is searched, never run.

## Vendored library

`windows/third_party/go-webview2` is a copy of `github.com/wailsapp/go-webview2` v1.0.23 (MIT). Patches
remove calls to `os.Exit` and `log.Fatal` that made any single WebView2 error close the whole browser, and
stop late resizes from touching a closed view; see `FASTY-PATCHES.md` there. WebView2 itself is Microsoft's
and is not redistributed. The Dictation Dune component (`windows/ui-src/dictation-dune.tsx`) is by Louis
Studios, used as supplied apart from removing its decorative drop shadow.

## Installer

`windows/installer/fasty.iss` (Inno Setup). Per-user by default, Start Menu entry, optional desktop
shortcut, optional entry in Windows *Default apps* (you still choose the default browser yourself), a clean
uninstall, and a check for the WebView2 Runtime.
