# Fasty for Windows

The Windows build lives in `windows/`. It is a separate Go module (`fastybrowser-windows`), so the Linux
build in `src/` is not touched by it and does not need it.

## What it is

Linux Fasty is Go on GTK 4 and WebKitGTK. WebKitGTK does not run on Windows, so the Windows build
uses **Microsoft Edge WebView2** (the Chromium-based web control that ships with Windows 11 and current
Windows 10) for pages, and draws its own tab strip and toolbar. It is pure Go: no cgo, no C compiler,
no Windows SDK.

This is a **preview** (version 0.1.0). Linux and Windows share the product and the settings ideas, not the
code. See [What is missing](#what-is-missing) for the gap.

## Requirements

- Windows 10 or Windows 11, 64-bit (x64). WebView2 supports both; **only Windows 11 has been tested** so far.
- The Microsoft Edge WebView2 Runtime. It is already present on Windows 11 and on up-to-date Windows 10.
  If it is missing: <https://go.microsoft.com/fwlink/p/?LinkId=2124703>.
- No admin rights are needed; the installer installs for the current user by default.

## What works (tested)

`fasty.exe --selftest` starts the real app against a local web server and checks, in order:

1. a page loads and its title reaches the tab strip;
2. Shield blocks requests to ad hosts (counted);
3. `window.open` and `target=_blank` links open a new tab;
4. typing an address opens it; back and forward work;
5. `javascript:` text typed in the address bar is searched, never run;
6. the new-tab page, the Settings tab and saving settings to `settings.json`;
7. idle tabs sleep and wake with their page;
8. the session is saved, and closing every tab leaves one start-page tab.

All 13 steps pass on Windows 11 (build 26300). The installer was also installed, launched and uninstalled there with no leftovers. Windows 10 has not been tested yet.

Features: tabs, address bar with search, Shield (built-in host list plus the optional StevenBlack list,
refreshed weekly), sleeping tabs (default 5 minutes, like Linux), session restore, settings (home page,
search engine, Shield, sleep time, restore), light and dark following Windows, and WebView2's built-in
downloads, find in page (`Ctrl+F`), printing and developer tools (`F12`).

Keys: `Ctrl+T` new tab, `Ctrl+W` close, `Ctrl+L` address bar, `Ctrl+R` or `F5` reload, `Ctrl+Tab` next tab,
`Alt+Left`/`Alt+Right` back and forward.

## What is missing

Compared with Linux, the Windows preview does **not** have: the adblock-rust engine (Shield uses host
lists, which block less than EasyList rules), passkeys, the authenticator, extensions, picture in
picture, the agent socket, bookmarks, history and a downloads panel of its own, a side tab column, the
YouTube ad handling, auto-update, and an ARM64 build. These are tracked as follow-ups, not hidden.

Pages are drawn by WebView2, which is Chromium. The Linux build's "no Chromium" statement does not
apply to Windows.

## Build

Needs Go 1.27 or newer. Inno Setup 6 is only needed for the installer
(`winget install JRSoftware.InnoSetup`).

```powershell
cd windows
.\build.ps1                      # vet, test, build dist\fasty.exe
.\build.ps1 -Installer -Zip      # also the installer and the portable zip
.\build.ps1 -SelfTest            # also run the in-app self-test
```

CI workflows are in `windows/ci/` until they can be copied to `.github/workflows/` (see its README).

Output: `windows/dist/fasty.exe` (about 7.5 MB), `windows/dist/installer/Fasty-Setup-<version>-x64.exe`
(about 4.3 MB) and `windows/dist/Fasty-<version>-x64-portable.zip`.

Measure memory and idle CPU with `windows\tools\bench.ps1 -SleepMinutes 0` and `-SleepMinutes 5`.

## Data

- Settings and session: `%AppData%\FastyBrowser\settings.json` and `session.json`.
- Web data (cookies, cache): `%LocalAppData%\FastyBrowser\WebView2`.
- Log: `%LocalAppData%\FastyBrowser\fasty.log`.

## Performance

Chromium flags `--disable-background-networking --no-pings --disable-breakpad` cut idle background work,
and sleeping tabs free the page of tabs you are not using. Measured on the development PC with five real
sites open (one run each, so treat as indicative):

| Setting | Working set | Private memory | Idle CPU (all cores) |
| --- | --- | --- | --- |
| Sleeping tabs off | 825 MB | 477 MB | 0.11 % |
| Sleeping tabs on (default) | 593 MB | 336 MB | 0.05 % |

Helium (<https://github.com/imputnet/helium>) patches the Chromium source tree. Fasty on Windows does not
build Chromium, so those patches do not apply; only runtime flags and tab sleeping are used here.

## Vendored library

`windows/third_party/go-webview2` is a copy of `github.com/wailsapp/go-webview2` v1.0.23 (MIT). Two
patches remove calls to `os.Exit` and `log.Fatal` that made any single WebView2 error close the whole
browser; see `FASTY-PATCHES.md` there. WebView2 itself is Microsoft's and is not redistributed.

## Installer

`windows/installer/fasty.iss` (Inno Setup). Per-user by default, Start Menu entry, optional desktop
shortcut, optional entry in Windows *Default apps* (you still choose the default browser yourself), a
clean uninstall, and a check for the WebView2 Runtime.
