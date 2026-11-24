# Agent control

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
| `close` | | close the selected tab (or extension window) |
| `back` / `forward` / `reload` | | navigation |
| `url` / `title` | | current page URL or title |
| `eval` | `script` | run JS, return the result |
| `content` | | `document.body.innerText` |
| `html` | | `document.documentElement.outerHTML` |
| `screenshot` | `path` | save PNG, return the path |
| `click` | `selector` | click the first matching element |
| `type` | `text` | resolve text as a URL or search and navigate |
| `wait` | | no-op reply, useful for checking the socket |
| `popups` | | extension windows, with `index`, `url`, `title`, `loading`, `built`, `selected` |
| `popup-select` | `tab` (1-based) | select an extension window for following commands (web pop-ups open as ordinary tabs) |
| `state` | | full browser state |
| `extensions` | | list installed extensions |
| `ext-install` | `url` / `path` / `id` | install an extension from a URL, folder or ID |
| `ext-remove` / `ext-toggle` / `ext-reload` | `id`, `enabled` | manage an extension |
| `ext-eval` / `ext-call` / `ext-open` / `ext-action` | `id`, `page`, `message`, `target`, `area` | run or query extension code |
| `ext-logs` / `ext-storage` | `id`, `items` | read extension logs or storage |
| `cookies` | `url` | cookies of a site: name, domain, path, flags; values hidden |
| `elements` | | clickable elements on the page with selectors |
| `record-start` / `record-stop` / `record-status` / `record-clear` | | network recording |
| `record-list` | `filter`, `method`, `limit`, `offset` | recorded requests; filter is URL text or `:failed` `:post` `:redirect` `:cookies` |
| `record-get` | `entry` | one request in full: headers, redirects, bodies |
| `record-export` | `path` | write the redacted recording to a private file |
| `help` | | list of actions |
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


## Recording network traffic

Settings → Privacy → Agent control has a **Record network traffic** button, and
`record-start` / `record-stop` do the same over the socket. While it is on,
every request from every tab is logged: method, URL, request and response
headers, redirects, status and failures, plus request bodies and fetch/XHR
responses seen by the page. Stopping from the button saves the log under
`~/.local/share/fasty-browser/recordings/`.

## Intercepting and changing requests

For testing your own site, or one you have permission to test: change what the
page sends and what the server answers, then see whether the server still
holds up. It works on requests the page's own script makes (`fetch`, XHR,
`sendBeacon`). Navigations, images, CSS and plain form posts are not touched,
and the browser won't let page script set `Cookie`, `Host`, `Origin`,
`Referer` or `User-Agent`.

| Action | Fields | What it does |
|---|---|---|
| `intercept-rule` | `rule` | change matching requests automatically |
| `intercept-hold` | `filter`, `method`, `timeout` | hold matching requests until you release them |
| `intercept-list` | | held requests, active rules |
| `intercept-forward` | `id`, `rule` | release a held request, with edits if given |
| `intercept-drop` | `id` | cancel a held request |
| `intercept-log` | | which rules fired |
| `intercept-stop` | | remove all rules and the hold, release what is held |

A rule needs `match` (a URL substring, or `re:` and a regular expression) and
can have `method`, `set_headers`, `remove_headers`, `url`, `url_replace`,
`body`, `body_replace`, `json_set` (dotted paths, e.g. `{"item.qty": 100}`),
`block`, `mock` (a fake response), `delay_ms`, and, for the answer coming
back, `resp_json_set`, `resp_body_replace`, `resp_status` and `resp_body` (`mock` and the `resp_*`
edits work on `fetch` only).

Typical checks: send a different quantity, price or stock value than the page
holds; make the page believe an out-of-stock product is in stock and see
whether checkout still refuses it; drop a header or change an ID to look for
missing permission checks. Use `record-start` and `record-get` to see what
reached the server and what it said. Held requests are let through unchanged
after `timeout` seconds (60 by default). These tests change real data on a
live site, so use a test account and test products.

## Speed

`clear-cache` empties the HTTP cache (logins stay). `perf` loads a page,
cold in a private tab by default, and returns timings, the slowest requests
and plain findings.

## Secrets are redacted

Every reply on the socket passes through a redactor (`redact.go`). Tokens,
API keys, passwords, session and CSRF cookies, `Authorization` headers and
OAuth codes are replaced with `[REDACTED N chars]`, so an agent can see what
was sent without seeing the secret. This cannot be turned off.
