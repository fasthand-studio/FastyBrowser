// Extension store pages and pages that talk to extensions.
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

var chromeIDRe = regexp.MustCompile(`[a-p]{32}`)

// InstallFromSource installs from a store page URL, a store extension id, a
// direct .crx/.xpi/.zip URL, or a local path.
func (m *ExtensionManager) InstallFromSource(src string, done func(name string, err error)) {
	src = strings.TrimSpace(src)
	switch {
	case strings.HasPrefix(src, "/") || strings.HasPrefix(src, "file://"):
		name, err := m.InstallFromFile(strings.TrimPrefix(src, "file://"))
		done(name, err)
	case strings.Contains(src, "addons.mozilla.org"):
		mt := regexp.MustCompile(`/addon/([^/?#]+)`).FindStringSubmatch(src)
		if mt == nil {
			done("", fmt.Errorf("not an add-on page: %s", src))
			return
		}
		m.installDownloaded("https://addons.mozilla.org/firefox/downloads/latest/"+mt[1]+"/addon-latest.xpi", ".xpi", mt[1], done)
	case strings.Contains(src, "chromewebstore.google.com") || strings.Contains(src, "chrome.google.com") || chromeIDRe.FindString(src) == src:
		id := chromeIDRe.FindString(src)
		if id == "" {
			done("", fmt.Errorf("no extension id in %s", src))
			return
		}
		m.installDownloaded(chromeStoreDownloadURL(id), ".crx", id, done)
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		ext := strings.ToLower(filepath.Ext(strings.SplitN(src, "?", 2)[0]))
		if ext != ".crx" && ext != ".xpi" && ext != ".zip" {
			done("", fmt.Errorf("unsupported url: %s", src))
			return
		}
		m.installDownloaded(src, ext, randomHex(4), done)
	default:
		done("", fmt.Errorf("don't know how to install %q", src))
	}
}

func (m *ExtensionManager) installDownloaded(dl, ext, id string, done func(string, error)) {
	tmp := filepath.Join(cacheDir(), "ext-install-"+sanitizeID(id)+ext)
	go func() {
		err := downloadFile(dl, tmp)
		coreglib.IdleAdd(func() {
			if err != nil {
				done("", err)
				return
			}
			name, err := m.InstallFromFile(tmp, id)
			os.Remove(tmp)
			done(name, err)
		})
	}()
}

// HandleStoreMessage serves the buttons the store helper adds to store pages:
// install, remove, and asking whether an extension is already installed.
func (m *ExtensionManager) HandleStoreMessage(body string, view *webkit.WebView) {
	var req struct {
		Op    string `json:"op"`
		Store string `json:"store"`
		ExtID string `json:"extId"`
		URL   string `json:"url"`
	}
	if json.Unmarshal([]byte(body), &req) != nil || req.ExtID == "" {
		return
	}
	state := func(installed bool) {
		m.evalIn(view, "", "window.__fastyStoreState&&window.__fastyStoreState("+jsq(installed)+")")
	}
	switch req.Op {
	case "status":
		state(m.find(sanitizeID(req.ExtID)) != nil)
	case "remove":
		id := sanitizeID(req.ExtID)
		if ext := m.find(id); ext != nil {
			m.Remove(id)
			if m.b != nil {
				m.b.rebuildExtensionButtons()
				m.b.toast("Removed "+ext.Manifest.Name, 2)
			}
		}
		state(false)
	default:
		m.HandleStoreInstall(body, nil, func(name string, err error) {
			if err != nil {
				if m.b != nil {
					m.b.toast("Extension install failed: "+err.Error(), 4)
				}
				state(false)
				return
			}
			if m.b != nil {
				m.b.rebuildExtensionButtons()
				m.b.toast("Installed \""+name+"\"", 3)
			}
			state(true)
		})
	}
}

func (m *ExtensionManager) findByRealID(id string) *Extension {
	if e := m.find(sanitizeID(id)); e != nil {
		return e
	}
	for _, e := range m.extensions {
		if e.realID() == id {
			return e
		}
	}
	return nil
}

// externallyConnectable lists, per extension, the pages that may message it.
func (m *ExtensionManager) injectWebConnect(ucm *webkit.UserContentManager) {
	for _, ext := range m.enabledList() {
		if ext.Manifest.Externally == nil || len(ext.Manifest.Externally.Matches) == 0 {
			continue
		}
		ucm.AddScript(webkit.NewUserScript(webConnectJS, webkit.UserContentInjectTopFrame,
			webkit.UserScriptInjectAtDocumentStart, matchPatternsToAllowList(ext.Manifest.Externally.Matches), nil))
		break
	}
}

const webConnectJS = `(function(){
  if (window.__fastyWebConnect) return; window.__fastyWebConnect = true;
  var pend = {}, seq = 0;
  window.__fastyExtReply = function (n, data, err) {
    var f = pend[n]; if (!f) return; delete pend[n]; f(data === null ? undefined : data, err || '');
  };
  var chromeObj = window.chrome || (window.chrome = {});
  var rt = chromeObj.runtime || (chromeObj.runtime = {});
  rt.sendMessage = function (extId, message, opts, cb) {
    if (typeof opts === 'function') { cb = opts; }
    var n = ++seq;
    var p = new Promise(function (resolve, reject) {
      pend[n] = function (d, e) {
        if (cb) { rt.lastError = e ? { message: e } : null; try { cb(d); } finally { rt.lastError = null; } resolve(d); }
        else if (e) reject(new Error(e)); else resolve(d);
      };
    });
    window.webkit.messageHandlers.officeVeil.postMessage({ _ch: 'extExt', extId: String(extId), message: message, cbId: n });
    return cb ? undefined : p;
  };
})();`

// HandleWebExternal serves runtime.sendMessage(extensionId, ...) from a web
// page the extension lists under externally_connectable.
func (m *ExtensionManager) HandleWebExternal(body string, t *Tab) {
	var req struct {
		ExtID   string          `json:"extId"`
		Message json.RawMessage `json:"message"`
		CbID    int             `json:"cbId"`
	}
	if json.Unmarshal([]byte(body), &req) != nil || t.view == nil {
		return
	}
	reply := func(d json.RawMessage, e string) {
		if len(d) == 0 {
			d = json.RawMessage("null")
		}
		m.evalIn(t.view, "", fmt.Sprintf("window.__fastyExtReply&&window.__fastyExtReply(%d,%s,%s)", req.CbID, string(d), jsq(e)))
	}
	ext := m.findByRealID(req.ExtID)
	allowed := false
	if ext != nil && ext.Enabled && ext.Manifest.Externally != nil {
		for _, p := range ext.Manifest.Externally.Matches {
			if re, err := regexp.Compile(matchPatternToRegex(p)); err == nil && re.MatchString(t.url) {
				allowed = true
			}
		}
	}
	if !allowed {
		reply(nil, "Could not establish connection. Receiving end does not exist.")
		return
	}
	sender := map[string]any{"url": t.url, "tab": m.tabInfo(t), "frameId": 0, "__external": true}
	if u, err := url.Parse(t.url); err == nil && u.Host != "" {
		sender["origin"] = u.Scheme + "://" + u.Host
	}
	m.route(m.bgEP(ext), req.Message, sender, reply)
}
