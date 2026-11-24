# Features in detail

## New windows open as tabs

Links with `target=_blank` and pages that call `window.open` open as a tab next
to the page that opened them, not as a separate window. The new tab stays
linked to its opener, so sign-in pop-ups that report back to the page, and
`window.close()`, still work.

## Site icons are kept

Fasty saves each site's icon (one small PNG per site) and uses it until the page
supplies a new one, so pinned and restored tabs keep their icons after a restart.
Private tabs don't use it.

## Signing in with Facebook

Instagram's "Log in with Facebook" asks facebook.com, from instagram.com, who is
signed in. That needs Facebook's cookies to be `SameSite=None`, but WebKitGTK
stores them as `Lax`, so the check came back empty and sign-in failed. Fasty
sets Meta's secure cookies (facebook.com, instagram.com, messenger.com,
threads.net) to `SameSite=None` again after they change. Other sites are left as
they set them (`metacookies.go`).

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
