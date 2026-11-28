//go:build windows

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/go-webview2/pkg/edge"
)

// ---- Lookup helpers -----------------------------------------------------------------------

func (a *app) byID(id int) *tab {
	for _, t := range a.tabs {
		if t.id == id {
			return t
		}
	}
	return nil
}

func (a *app) indexOf(t *tab) int {
	for i, x := range a.tabs {
		if x == t {
			return i
		}
	}
	return -1
}

// ---- Opening, closing and switching ---------------------------------------------------------

func (a *app) restoreOrOpen(urls []string) {
	if len(urls) == 0 && a.settings.RestoreSession {
		if s, err := LoadSession(a.dataDir); err == nil {
			for i, u := range s.URLs {
				t := a.addTab(kindPage, u, -1, false)
				if i < len(s.Pinned) {
					t.pinned = s.Pinned[i]
				}
				if i == s.Active {
					a.active = t
				}
			}
			a.groupPinned()
		}
	}
	for _, u := range urls {
		a.addTab(kindPage, ResolveInput(u, a.settings.SearchEngine), -1, false)
	}
	if p := os.Getenv("FASTY_OPEN_INTERNAL"); p != "" { // developer aid
		a.addTab(kindInternal, p, -1, false)
	}
	if len(a.tabs) == 0 {
		a.addTab(kindInternal, "newtab.html", -1, false)
	}
	if a.active == nil {
		a.active = a.tabs[0]
	}
	a.active.lastActive = time.Now()
}

func (a *app) saveSession() {
	var s Session
	for _, t := range a.tabs {
		if t.kind == kindPage && !t.private && t.url != "" && t.url != "about:blank" {
			if t == a.active {
				s.Active = len(s.URLs)
			}
			s.URLs = append(s.URLs, t.url)
			s.Pinned = append(s.Pinned, t.pinned)
		}
	}
	if len(s.URLs) > 0 {
		_ = SaveSession(a.dataDir, s)
	} else {
		_ = os.Remove(filepath.Join(a.dataDir, "session.json"))
	}
}

// openInternal opens (or switches to) one of the browser's own pages.
func (a *app) openInternal(page string, reuse bool) *tab {
	if reuse {
		name := strings.SplitN(page, "?", 2)[0]
		for _, t := range a.tabs {
			if t.kind == kindInternal && strings.SplitN(t.page, "?", 2)[0] == name {
				a.activate(t)
				return t
			}
		}
	}
	return a.newTab(kindInternal, page, false)
}

func (a *app) newTab(kind tabKind, urlOrPage string, private bool) *tab {
	t := a.addTab(kind, urlOrPage, -1, private)
	a.activate(t)
	return t
}

// addTab creates the tab and queues its web view. index < 0 appends.
func (a *app) addTab(kind tabKind, urlOrPage string, index int, private bool) *tab {
	a.nextID++
	t := &tab{id: a.nextID, kind: kind, url: urlOrPage, pendingURL: urlOrPage, lastActive: time.Now(), private: private}
	if kind == kindInternal {
		t.page, t.url, t.pendingURL = urlOrPage, "", ""
	}
	v := edge.NewChromium()
	v.DataPath = a.webviewDir
	if private {
		// A private tab is its own browser profile: separate cookies and storage, deleted when it closes.
		t.privDir = filepath.Join(os.TempDir(), "fasty-private", time.Now().Format("150405.000")+"-"+itoa(t.id))
		_ = os.MkdirAll(t.privDir, 0o700)
		v.DataPath = t.privDir
	}
	v.SetErrorCallback(func(err error) { log.Println("tab view:", err) })
	v.MessageCallback = func(msg string, _ *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		src, _ := args.GetSource()
		a.onTabMessage(t, msg, src)
	}
	v.WebResourceRequestedCallback = func(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
		a.onResource(t, req, args)
	}
	v.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		t.loading = false
		a.syncChrome()
	}
	v.ProcessFailedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2ProcessFailedEventArgs) {
		// The page process died; leave the tab as it is, a reload brings it back.
		log.Println("page process failed in tab", t.id)
		t.loading = false
	}
	v.ContainsFullScreenElementChangedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2ContainsFullScreenElementChangedEventArgs) {
		// A page asked for full screen (a video, a game): give it the whole window.
		a.runOnUI(func() { a.setFullscreen(!a.fullscreen) })
	}
	t.view = v
	if index < 0 || index > len(a.tabs) {
		a.tabs = append(a.tabs, t)
	} else {
		a.tabs = append(a.tabs[:index], append([]*tab{t}, a.tabs[index:]...)...)
	}
	// Embed runs its own message loop until the view exists, and WebView2 deadlocks if that happens
	// inside one of its event callbacks (a page message, a click in the tab strip). So the view is
	// created from a posted message, on the next turn of the main loop.
	a.embedQueue = append(a.embedQueue, t)
	procPostMessageW.Call(a.hwnd, wmEmbed, 0, 0)
	return t
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (a *app) embedPending() {
	q := a.embedQueue
	a.embedQueue = nil
	for _, t := range q {
		if a.indexOf(t) < 0 || a.quitting { // closed before its view was made
			continue
		}
		h, ok := a.embedView(t.view)
		if !ok {
			log.Println("could not create a page view for tab", t.id)
			a.closeTab(t)
			continue
		}
		t.hwnd = h
	}
}

func (a *app) activate(t *tab) {
	if t == nil || a.indexOf(t) < 0 {
		return
	}
	prev := a.active
	a.active = t
	t.lastActive = time.Now()
	if prev != nil && prev != t && prev.ready {
		_ = prev.view.GetController().PutIsVisible(false)
	}
	if t.ready {
		_ = t.view.GetController().PutIsVisible(true)
		a.layout()
		if t.asleep && t.url != "" {
			t.asleep = false
			t.loading = true
			t.view.Navigate(t.url)
		}
		t.view.Focus()
	}
	a.setWindowTitle()
	a.syncChrome()
}

func (a *app) cycle(d int) {
	if len(a.tabs) < 2 || a.active == nil {
		return
	}
	i := (a.indexOf(a.active) + d + len(a.tabs)) % len(a.tabs)
	a.activate(a.tabs[i])
}

func (a *app) closeTab(t *tab) {
	i := a.indexOf(t)
	if i < 0 {
		return
	}
	if t.kind == kindPage && t.url != "" && !t.private {
		a.closed = append(a.closed, closedTab{url: t.url, pinned: t.pinned})
		if len(a.closed) > 20 {
			a.closed = a.closed[1:]
		}
	}
	if t.view != nil {
		t.view.ShuttingDown()
		if t.ready {
			_ = t.view.GetController().PutIsVisible(false)
			closeController(t.view.GetController())
		}
	}
	a.removePrivateData(t)
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if len(a.tabs) == 0 {
		a.newTab(kindInternal, "newtab.html", false)
		return
	}
	if a.active == t {
		a.active = nil
		next := i
		if next >= len(a.tabs) {
			next = len(a.tabs) - 1
		}
		a.activate(a.tabs[next])
		return
	}
	a.syncChrome()
}

// removePrivateData deletes a private tab's profile once its web process has let go of it.
func (a *app) removePrivateData(t *tab) {
	if t.privDir == "" {
		return
	}
	dir := t.privDir
	t.privDir = ""
	go func() {
		for i := 0; i < 10; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func (a *app) reopenClosed() {
	n := len(a.closed)
	if n == 0 {
		return
	}
	c := a.closed[n-1]
	a.closed = a.closed[:n-1]
	t := a.newTab(kindPage, c.url, false)
	if c.pinned {
		t.pinned = true
		a.groupPinned()
		a.syncChrome()
	}
}

func (a *app) duplicate(t *tab) {
	if t == nil || t.kind != kindPage || t.url == "" {
		return
	}
	nt := a.addTab(kindPage, t.url, a.indexOf(t)+1, t.private)
	a.activate(nt)
}

func (a *app) togglePin(t *tab) {
	if t == nil {
		return
	}
	t.pinned = !t.pinned
	a.groupPinned()
	a.syncChrome()
}

// groupPinned keeps pinned tabs first, in the order they were pinned.
func (a *app) groupPinned() {
	var pins, rest []*tab
	for _, t := range a.tabs {
		if t.pinned {
			pins = append(pins, t)
		} else {
			rest = append(rest, t)
		}
	}
	a.tabs = append(pins, rest...)
}

func (a *app) moveTab(id, before int) {
	t, b := a.byID(id), a.byID(before)
	if t == nil || b == nil || t == b {
		return
	}
	a.tabs = append(a.tabs[:a.indexOf(t)], a.tabs[a.indexOf(t)+1:]...)
	i := a.indexOf(b)
	a.tabs = append(a.tabs[:i], append([]*tab{t}, a.tabs[i:]...)...)
	t.pinned = b.pinned // dropping onto a pinned tab pins, onto a normal one unpins
	a.groupPinned()
	a.syncChrome()
}

func (a *app) closeOthers(keep *tab) {
	for _, t := range append([]*tab(nil), a.tabs...) {
		if t != keep && !t.pinned {
			a.closeTab(t)
		}
	}
}

// pollReady finishes setting up views once WebView2 has created their controllers.
func (a *app) pollReady() {
	if a.chrome != nil && !a.chromeReady && a.chrome.GetController() != nil {
		a.chromeReady = true
		a.setupCommon(a.chrome)
		a.chrome.Navigate(ui.URL("chrome.html?t=" + a.themeName()))
		a.layout()
	}
	for _, t := range a.tabs {
		if t.ready || t.view.GetController() == nil {
			continue
		}
		t.ready = true
		a.setupTab(t)
	}
}

func (a *app) setupCommon(v *edge.Chromium) {
	if st, err := v.GetSettings(); err == nil {
		_ = st.PutIsWebMessageEnabled(true)
		_ = st.PutIsStatusBarEnabled(false)
	}
	v.SetBackgroundColour(uint8(a.pal().frame>>16), uint8(a.pal().frame>>8), uint8(a.pal().frame), 255)
}

func (a *app) internalURL(page string) string {
	sep := "?"
	if strings.Contains(page, "?") {
		sep = "&"
	}
	return ui.URL(page + sep + "t=" + a.themeName())
}

func (a *app) setupTab(t *tab) {
	v := t.view
	a.setupCommon(v)
	isActive := t == a.active
	_ = v.GetController().PutIsVisible(isActive)
	switch t.kind {
	case kindPage:
		v.Init(pageScript)
		// Block ads and trackers on subresources only; the page itself always loads.
		for _, ctx := range []edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT{
			edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_IMAGE, edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_MEDIA,
			edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_SCRIPT, edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_XML_HTTP_REQUEST,
			edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_FETCH, edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_PING,
			edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_OTHER,
		} {
			v.AddWebResourceRequestedFilter("*", ctx)
		}
		if t.pendingURL != "" {
			if isActive || a.settings.SleepMinutes == 0 {
				t.loading = true
				v.Navigate(t.pendingURL)
			} else {
				t.asleep = true // restored background tabs stay asleep until opened
			}
		}
	case kindInternal:
		// Only the browser's own pages get the microphone, and without a prompt.
		v.SetPermission(edge.CoreWebView2PermissionKindMicrophone, edge.CoreWebView2PermissionStateAllow)
		v.Navigate(a.internalURL(t.page))
	}
	t.pendingURL = ""
	a.layout()
	if isActive {
		v.Focus()
	}
	a.syncChrome()
}

func (a *app) sleepIdleTabs() {
	if a.settings.SleepMinutes <= 0 {
		return
	}
	limit := time.Duration(a.settings.SleepMinutes) * time.Minute
	changed := false
	for _, t := range a.tabs {
		if t == a.active || t.kind != kindPage || !t.ready || t.asleep || t.url == "" || t.pinned && false {
			continue
		}
		if time.Since(t.lastActive) >= limit {
			t.asleep = true
			t.view.Navigate("about:blank") // frees the page; its address is kept for waking
			changed = true
		}
	}
	if changed {
		a.syncChrome()
	}
}

// ---- Page messages and navigation -----------------------------------------------------------

type pageMsg struct {
	A  string          `json:"a"`
	T  string          `json:"t"`
	U  string          `json:"u"`
	K  string          `json:"k"`
	V  string          `json:"v"`
	I  string          `json:"i"`
	CB bool            `json:"cb"`
	CF bool            `json:"cf"`
	ID json.Number     `json:"id"`
	M  string          `json:"m"`
	P  json.RawMessage `json:"p"`
	On bool            `json:"on"`
}

func (a *app) onTabMessage(t *tab, raw string, src string) {
	var m pageMsg
	if json.Unmarshal([]byte(raw), &m) != nil || a.indexOf(t) < 0 {
		return
	}
	internal := t.kind == kindInternal && ui.IsInternal(src)
	switch {
	case m.A == "meta" && t.kind == kindPage:
		if t.asleep || m.U == "about:blank" {
			return
		}
		changedURL := t.url != m.U
		t.title, t.url, t.canBack, t.canFwd, t.loading = m.T, m.U, m.CB, m.CF, false
		if m.I != "" {
			t.favicon = m.I
		} else if changedURL {
			t.favicon = ""
		}
		if !t.private {
			if t.recorded != m.U {
				t.recorded = m.U
				a.history.Record(m.U, m.T)
			}
			a.history.SetTitle(m.U, m.T)
		}
		if t == a.active {
			a.setWindowTitle()
		}
		a.syncChrome()
	case m.A == "loading" && t.kind == kindPage:
		t.loading = m.On
		a.syncChrome()
	case m.A == "open" && t.kind == kindPage:
		if IsAddress(m.U) {
			a.newTab(kindPage, CleanURL(m.U, a.settings.CleanLinks), t.private)
		}
	case m.A == "reader" && t.kind == kindPage:
		if m.V == "read" {
			t.reading = true
		} else {
			a.toast("Nothing here reads like an article")
		}
	case m.A == "key":
		a.shortcut(m.K)
	case m.A == "title" && internal:
		t.title = m.T
		if t == a.active {
			a.setWindowTitle()
		}
		a.syncChrome()
	case m.A == "log" && internal:
		if k, v, ok := strings.Cut(strings.TrimPrefix(m.V, "flag:"), "="); ok && strings.HasPrefix(m.V, "flag:") {
			a.selfTestFlag(k, v)
		} else {
			log.Println("page:", m.V)
		}
	case m.A == "rpc" && internal:
		a.handleRPC(t, m)
	}
}

func (a *app) evalActive(js string) {
	if t := a.active; t != nil && t.kind == kindPage && t.ready && !t.asleep {
		t.view.Eval(js)
	}
}

func (a *app) goBack()    { a.evalActive("history.back()") }
func (a *app) goForward() { a.evalActive("history.forward()") }

func (a *app) reloadTab(t *tab, hard bool) {
	if t == nil || t.kind != kindPage || !t.ready {
		return
	}
	if t.asleep {
		t.asleep = false
		t.loading = true
		t.view.Navigate(t.url)
		a.syncChrome()
		return
	}
	t.loading = true
	if hard {
		t.view.Eval("location.reload(true)")
	} else {
		t.view.Eval("location.reload()")
	}
	a.syncChrome()
}

func (a *app) stopLoading() {
	if t := a.active; t != nil && t.kind == kindPage && t.ready {
		t.view.Eval("window.stop()")
		t.loading = false
		a.syncChrome()
	}
}

// navigate loads what was typed into the address field or the new-tab box. Internal pages are
// replaced by a page tab in the same position, because only page tabs carry the page script.
func (a *app) navigate(t *tab, input string) {
	u := CleanURL(ResolveInput(input, a.settings.SearchEngine), a.settings.CleanLinks && IsAddress(input))
	if u == "" || t == nil {
		return
	}
	if t.kind != kindPage {
		idx := a.indexOf(t)
		nt := a.addTab(kindPage, u, idx+1, t.private)
		a.activate(nt)
		a.closeTab(t)
		return
	}
	t.url, t.title, t.asleep, t.loading, t.favicon = u, "", false, true, ""
	if t.ready {
		t.view.Navigate(u)
	} else {
		t.pendingURL = u
	}
	a.syncChrome()
}

// onResource answers blocked requests with an empty 403 so the page moves on at once.
func (a *app) onResource(t *tab, req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
	if !a.settings.Shield || a.quitting {
		return
	}
	uri, err := req.GetUri()
	if err != nil || !a.shield.Blocked(uri) {
		return
	}
	env := t.view.Environment()
	if env == nil {
		return
	}
	resp, err := env.CreateWebResourceResponse([]byte{' '}, http.StatusForbidden, "Blocked by Shield", "")
	if err != nil {
		return
	}
	if args.PutResponse(resp) == nil {
		a.shield.Hit()
	}
}

// CleanURL removes tracking parameters (utm_*, fbclid, gclid ...) when enabled.
func CleanURL(raw string, on bool) string {
	if !on {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery == "" {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(k)
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
}

var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "dclid": true, "gbraid": true, "wbraid": true, "msclkid": true,
	"mc_eid": true, "mc_cid": true, "igshid": true, "yclid": true, "_hsenc": true, "_hsmi": true,
	"mkt_tok": true, "oly_enc_id": true, "oly_anon_id": true, "vero_id": true, "ref_src": true,
}

// loadExtendedShield reads the cached StevenBlack hosts list and refreshes it in the
// background when it is missing or older than a week.
func (a *app) loadExtendedShield() {
	if !a.settings.Shield || !a.settings.ExtendedShield {
		return
	}
	path := filepath.Join(a.dataDir, "hosts.txt")
	if f, err := os.Open(path); err == nil {
		n := a.shield.LoadHosts(f)
		f.Close()
		log.Printf("shield: loaded %d hosts from cache", n)
	}
	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < 7*24*time.Hour {
		return
	}
	go func() {
		const src = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
		client := &http.Client{Timeout: 45 * time.Second}
		resp, err := client.Get(src)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			log.Println("shield: list download failed:", err)
			return
		}
		defer resp.Body.Close()
		tmp := path + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			return
		}
		_, cerr := copyLimited(out, resp.Body, 32<<20)
		out.Close()
		if cerr != nil {
			os.Remove(tmp)
			return
		}
		if os.Rename(tmp, path) == nil {
			if f, err := os.Open(path); err == nil {
				n := a.shield.LoadHosts(f)
				f.Close()
				log.Printf("shield: refreshed list, %d hosts", n)
			}
		}
	}()
}
