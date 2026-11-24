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
engine with GTK 4 and libadwaita: no bundled Chromium and no Electron.

- **Light on memory.** Tabs you aren't using go to sleep. [Details](docs/performance.md)
- **Blocks ads and trackers itself**, before a request is made, with no
  extension and no remote service. [How](docs/blocking.md)
- **Passkeys and an Authenticator** for one-time codes, stored encrypted on
  your computer. [Details](docs/features.md)
- **Picture in picture** that plays only the video, on every video.
- **Extensions**: WebExtension-style add-ons and an extension store.
- **Agent control**: a script or AI agent can read pages, click and run
  JavaScript over a local socket, with secrets redacted. [Details](docs/agent.md)
- **Built for security testing with AI**: record every request, then change
  requests and responses on the fly (edit a body field, flip "out of stock"
  to "in stock", block, delay, hold and edit by hand) to check that your own
  site's server doesn't trust the browser. Also clears the cache and measures
  page speed. For sites you own or are allowed to test. [Details](docs/agent.md#intercepting-and-changing-requests)
- **Private by design**: no account, no telemetry, everything stays on your
  machine. [Details](docs/privacy.md)

## Install

Download the `.deb`, `.rpm`, AppImage or tarball from the
[latest release](https://github.com/fasthand-studio/FastyBrowser/releases/latest).
For the tarball, unpack it and run `./install.sh`.

To build from source:

```bash
./install.sh     # builds and installs to ~/.local, no root needed
fasty-browser
```

Needs Go, Rust, `curl`, `python3`, WebKitGTK 6.0 and GStreamer plugins, and a
system with glibc 2.39 or newer (Ubuntu 24.04+, Debian 13+, Fedora 40+,
current Arch). Full steps and package names: [docs/install.md](docs/install.md).

## Keyboard shortcuts

`Ctrl+L` address bar, `Ctrl+K` switch tab, `Ctrl+T` new tab, `Ctrl+W` close
tab, `Ctrl+F` find, `Ctrl+Shift+R` reader mode, `Ctrl+Shift+P` Picture in
picture, `Ctrl+,` settings. Press `Ctrl+?` in the app for the full list.

## Known issues and contributing

Fasty is young, and it is a WebKit browser, not a Chromium one. Some
extensions may not work, because only part of the WebExtension APIs is
implemented, and you may run into glitches on some sites. If you find one,
please [open an issue](https://github.com/fasthand-studio/FastyBrowser/issues)
with the site or extension, what you expected, and what happened.

Developers are welcome: fix a bug, improve extension support, add a site fix
to `assets/js/compat.js`, or send a pull request. Start with
[docs/development.md](docs/development.md).

## Documentation

[Install](docs/install.md) · [Privacy and data](docs/privacy.md) ·
[Ad blocking](docs/blocking.md) · [Features](docs/features.md) ·
[Agent control](docs/agent.md) · [Memory and speed](docs/performance.md) ·
[Development](docs/development.md)

## License

MIT, see [LICENSE](LICENSE).

---

<p align="center"><sub>Made by <strong>FastHand Studio</strong></sub></p>
