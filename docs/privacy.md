# Privacy and data

**Fasty does not collect any data.** There is no account, telemetry, crash
reporting, analytics or remote configuration. Nothing is sent to FastHand
Studio or any other server by the browser itself. The only network requests
Fasty makes on its own are the public filter-list downloads described in [blocking.md](blocking.md);
everything else is a page you chose to open (including your chosen search
engine and any extension you installed).

Your data stays on this machine:

- History, bookmarks, settings, permissions, hidden elements and the open-tab
  session are JSON files in `~/.local/share/fasty-browser/`.
- Passkeys (`passkeys.json`) and Authenticator accounts (`authenticator.json`)
  are in the same folder, **encrypted** (see [features](features.md)). The keys that open them
  are kept in your system keyring (`secret-tool`/libsecret, such as KDE Wallet
  or GNOME Keyring), or in a file readable only by you if there is no keyring.
- A copy of each site's icon is kept in `favicons/` in the same folder so tabs
  don't lose their icons; clearing everything in Settings removes it.
- A copy of each site's icon is kept in `favicons/` in the same folder so tabs
  don't lose their icons; "Clear everything" in Settings removes it.
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
  mind when granting local access. Everything it sends back is redacted
  (tokens, keys, passwords, cookie values; see [agent.md](agent.md)). Network
  recording, when you switch it on, is stored locally under `recordings/`.

## Where the data is

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
