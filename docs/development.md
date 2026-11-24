# Development

Everything is one Go package (`package main`), split into small files by job. Related files share a prefix.

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

gpudecode.go          the GPU video decoding option (see [features](features.md))
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
at the top of `selftest.go`. The agent socket ([agent.md](agent.md)) is the other way to test
a running browser.

## Releases

`packaging/make-release.sh` builds the tarball and `SHA256SUMS` into `dist/`; `packaging/make-packages.sh` then builds the `.deb`, `.rpm` and AppImage from it (needs `nfpm` and `appimagetool`). The version is in `store.go`.
