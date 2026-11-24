# Installing

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

`build.sh` compiles the ad blocker into the browser; see [blocking.md](blocking.md).

YouTube can lose its picture while keeping audio after an ad when hardware
video decoding is used on some drivers. Fasty demotes the hardware decoders
(NVDEC, VA-API, VDPAU) so software decoding takes over, while GPU compositing
stays on. Set `GST_PLUGIN_FEATURE_RANK` yourself to override this.

GPU decoding is an option, off by default: see [features.md](features.md).

## From a release

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
