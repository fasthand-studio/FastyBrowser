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

## Secrets are redacted

Every reply on the socket passes through a redactor (`redact.go`). Tokens,
API keys, passwords, session and CSRF cookies, `Authorization` headers and
OAuth codes are replaced with `[REDACTED N chars]`, so an agent can see what
was sent without seeing the secret. This cannot be turned off.
