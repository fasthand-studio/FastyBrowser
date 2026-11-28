//go:build windows

package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
)

// ---- The sidebar / toolbar view (ui/chrome.html) ---------------------------------------------

func (a *app) startChrome() {
	v := edge.NewChromium()
	v.DataPath = a.webviewDir
	v.SetErrorCallback(func(err error) { log.Println("chrome view:", err) })
	v.MessageCallback = func(msg string, _ *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		if src, _ := args.GetSource(); ui.IsInternal(src) {
			a.onChromeMessage(msg)
		}
	}
	a.chrome = v
	h, ok := a.embedView(v)
	if !ok {
		a.fatal("Could not start the WebView2 runtime. Install it from https://go.microsoft.com/fwlink/p/?LinkId=2124703 and try again.")
	}
	a.chromeHwnd = h
}

type chromeMsg struct {
	A      string `json:"a"`
	V      string `json:"v"`
	Q      string `json:"q"`
	Mode   string `json:"mode"`
	URL    string `json:"url"`
	K      string `json:"k"`
	ID     int    `json:"id"`
	Before int    `json:"before"`
	D      int    `json:"d"`
	N      int    `json:"n"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Live   bool   `json:"live"`
}

func (a *app) onChromeMessage(raw string) {
	var m chromeMsg
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	switch m.A {
	case "ready":
		a.syncChrome()
	case "nav":
		a.navigate(a.active, m.V)
	case "suggest":
		a.sendSuggestions(m.Q, m.Mode)
	case "switchurl":
		for _, t := range a.tabs {
			if t.url == m.URL {
				a.activate(t)
				return
			}
		}
		a.navigate(a.active, m.URL)
	case "focuspage":
		if a.active != nil && a.active.ready {
			a.active.view.Focus()
		}
	case "overlay":
		a.overlayDIP = m.H
		a.layout()
	case "sidew":
		a.dragSidebar(m.W, m.Live)
	case "close":
		a.closeTab(a.byID(m.ID))
	case "switch":
		a.activate(a.byID(m.ID))
	case "move":
		a.moveTab(m.ID, m.Before)
	case "pin":
		a.togglePin(a.byID(m.ID))
	case "duplicate":
		if m.ID != 0 {
			a.duplicate(a.byID(m.ID))
		} else {
			a.duplicate(a.active)
		}
	case "reloadtab":
		a.reloadTab(a.byID(m.ID), false)
	case "closeothers":
		a.closeOthers(a.byID(m.ID))
	case "key":
		a.shortcut(m.K)
	default:
		a.command(m.A, m)
	}
}

// command runs a menu or toolbar action by name.
func (a *app) command(name string, m chromeMsg) {
	switch name {
	case "back":
		a.goBack()
	case "fwd":
		a.goForward()
	case "reload":
		a.reloadTab(a.active, false)
	case "reloadhard":
		a.reloadTab(a.active, true)
	case "stop":
		a.stopLoading()
	case "newtab":
		a.showOverlay(overlayParams{Mode: "newtab", Private: false})
	case "privatetab":
		a.showOverlay(overlayParams{Mode: "newtab", Private: true})
	case "spotlight":
		a.openSpotlight(m.Mode, false)
	case "reopen":
		a.reopenClosed()
	case "closeactive":
		a.closeTab(a.active)
	case "history":
		a.openInternal("history.html", true)
	case "bookmarks":
		a.openInternal("bookmarks.html", true)
	case "downloads":
		shellOpen(downloadsDir())
	case "settings":
		a.openSettings("")
	case "authenticator":
		a.openInternal("authenticator.html", true)
	case "hide":
		a.evalActive("window.__fastyPick&&window.__fastyPick()")
	case "hidden":
		a.openSettings("privacy")
	case "pauseshield":
		a.pauseShieldHere()
	case "defaultbrowser":
		shellOpen("ms-settings:defaultapps")
	case "shortcuts":
		a.openInternal("shortcuts.html", true)
	case "voice":
		a.openVoice()
	case "bookmark":
		a.toggleBookmark()
	case "reader":
		a.toggleReader()
	case "find":
		a.evalActive("") // WebView2's own find bar; Ctrl+F is handled by the browser control
		if a.active != nil && a.active.ready {
			a.active.view.Focus()
		}
	case "print":
		a.evalActive("window.print()")
	case "inspector":
		if t := a.active; t != nil && t.ready {
			t.view.OpenDevToolsWindow()
		}
	case "tabsside":
		a.setSetting(func(s *Settings) {
			if s.TabsSide == "left" {
				s.TabsSide = "top"
			} else {
				s.TabsSide = "left"
			}
		})
	case "fold":
		a.setSetting(func(s *Settings) { s.SidebarFolded = !s.SidebarFolded })
	case "switchpanel":
		a.focusAddress("switch")
	case "cycle":
		a.cycle(m.D)
	case "jump":
		if m.N >= 1 && m.N <= len(a.tabs) {
			target := a.tabs[m.N-1]
			if m.N == 9 {
				target = a.tabs[len(a.tabs)-1]
			}
			a.activate(target)
		}
	}
}

func downloadsDir() string {
	if p := os.Getenv("USERPROFILE"); p != "" {
		return filepath.Join(p, "Downloads")
	}
	return `C:\`
}

// shortcut maps a key combination (as sent by pages and by the chrome) to an action, using the
// same keys as the Linux build.
func (a *app) shortcut(k string) {
	switch k {
	case "ctrl+t":
		a.command("newtab", chromeMsg{})
	case "ctrl+shift+t":
		a.command("reopen", chromeMsg{})
	case "ctrl+shift+n":
		a.command("privatetab", chromeMsg{})
	case "ctrl+w":
		a.command("closeactive", chromeMsg{})
	case "ctrl+l":
		a.focusAddress("")
	case "ctrl+k":
		a.focusAddress("switch")
	case "ctrl+d":
		a.duplicate(a.active)
	case "ctrl+y":
		a.command("history", chromeMsg{})
	case "ctrl+alt+b":
		a.command("bookmarks", chromeMsg{})
	case "ctrl+shift+b":
		a.command("bookmark", chromeMsg{})
	case "ctrl+shift+j":
		a.command("downloads", chromeMsg{})
	case "ctrl+shift+h":
		a.command("hide", chromeMsg{})
	case "ctrl+shift+u":
		a.command("hidden", chromeMsg{})
	case "ctrl+shift+a":
		a.command("authenticator", chromeMsg{})
	case "ctrl+,":
		a.command("settings", chromeMsg{})
	case "ctrl+r", "f5":
		a.command("reload", chromeMsg{})
	case "ctrl+alt+r", "ctrl+f5":
		a.command("reloadhard", chromeMsg{})
	case "ctrl+shift+r":
		a.command("reader", chromeMsg{})
	case "ctrl+shift+s":
		a.command("tabsside", chromeMsg{})
	case "ctrl+s":
		a.command("fold", chromeMsg{})
	case "ctrl+tab", "ctrl+shift+bracketright":
		a.cycle(1)
	case "ctrl+shift+tab", "ctrl+shift+bracketleft":
		a.cycle(-1)
	case "alt+arrowleft", "ctrl+bracketleft":
		a.goBack()
	case "alt+arrowright", "ctrl+bracketright":
		a.goForward()
	case "f11":
		a.setFullscreen(!a.fullscreen)
	case "ctrl+shift+c":
		if t := a.active; t != nil && t.kind == kindPage {
			clipboardSet(CleanURL(t.url, true))
		}
	default:
		if len(k) == 6 && strings.HasPrefix(k, "ctrl+") && k[5] >= '1' && k[5] <= '9' {
			a.command("jump", chromeMsg{N: int(k[5] - '0')})
		}
	}
}

func (a *app) focusAddress(mode string) {
	if mode == "switch" {
		a.openSpotlight("switch", false)
		return
	}
	a.openSpotlight("address", false)
	if true {
		return
	}
	if a.chrome == nil || !a.chromeReady {
		return
	}
	if a.settings.SidebarFolded && a.settings.TabsSide == "left" {
		a.setSetting(func(s *Settings) { s.SidebarFolded = false })
	}
	a.chrome.Eval("fasty.setMode('" + mode + "');fasty.focusAddr(true)")
	a.chrome.Focus()
}

func (a *app) dragSidebar(widthPx int, live bool) {
	if a.settings.TabsSide != "left" {
		return
	}
	dip := int(float64(widthPx) / a.scale)
	if dip < 180 {
		dip = 180
	}
	if dip > 420 {
		dip = 420
	}
	if live {
		a.sideLive = dip
		a.layout()
		return
	}
	a.sideLive = 0
	a.setSetting(func(s *Settings) { s.SidebarWidth = dip })
}

// setSetting changes settings, saves them and refreshes the layout.
func (a *app) setSetting(fn func(*Settings)) {
	fn(&a.settings)
	a.settings = a.settings.Normalize()
	CustomSearch = a.settings.CustomEngine
	if err := SaveSettings(a.dataDir, a.settings); err != nil {
		log.Println("save settings:", err)
	}
	a.applyTheme()
	a.layout()
	a.syncChrome()
}

// ---- State pushed to the chrome ---------------------------------------------------------------

type tabInfo struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Favicon string `json:"favicon"`
	Pinned  bool   `json:"pinned"`
	Private bool   `json:"private"`
	Loading bool   `json:"loading"`
	Asleep  bool   `json:"asleep"`
}

type chromeSettings struct {
	TabsSide      string `json:"tabsSide"`
	SidebarFolded bool   `json:"sidebarFolded"`
	SidebarWidth  int    `json:"sidebarWidth"`
}

type chromeState struct {
	Tabs          []tabInfo      `json:"tabs"`
	Active        int            `json:"active"`
	URL           string         `json:"url"`
	Settings      chromeSettings `json:"settings"`
	Blocked       int64          `json:"blocked"`
	ShieldOn      bool           `json:"shieldOn"`
	Theme         string         `json:"theme"`
	Bookmarked    bool           `json:"bookmarked"`
	CanBack       bool           `json:"canBack"`
	CanForward    bool           `json:"canForward"`
	LoadingActive bool           `json:"loadingActive"`
	Internal      bool           `json:"internal"`
}

func (a *app) syncChrome() {
	if a.chrome == nil || !a.chromeReady || a.quitting {
		return
	}
	st := chromeState{
		Blocked: a.shield.Count(), ShieldOn: a.settings.Shield, Theme: a.themeName(),
		Settings: chromeSettings{a.settings.TabsSide, a.settings.SidebarFolded, a.settings.SidebarWidth},
	}
	for _, t := range a.tabs {
		title := t.title
		url := t.url
		if t.kind == kindInternal {
			url = ""
		}
		st.Tabs = append(st.Tabs, tabInfo{ID: t.id, Title: title, URL: url, Favicon: t.favicon,
			Pinned: t.pinned, Private: t.private, Loading: t.loading, Asleep: t.asleep})
	}
	if t := a.active; t != nil {
		st.Active = t.id
		st.Internal = t.kind == kindInternal
		st.LoadingActive = t.loading
		if t.kind == kindPage {
			st.URL = DisplayURL(t.url)
			st.Bookmarked = a.bookmarks.Has(t.url)
			st.CanBack, st.CanForward = t.canBack, t.canFwd
		}
	}
	b, _ := json.Marshal(st)
	a.chrome.Eval("window.fasty&&window.fasty.update(" + string(b) + ")")
	a.lastBlocked = st.Blocked
}

func (a *app) syncIfShieldChanged() {
	if a.shield.Count() != a.lastBlocked {
		a.syncChrome()
	}
}

func (a *app) setWindowTitle() {
	title := "Fasty"
	if a.active != nil && a.active.title != "" {
		title = a.active.title + " - Fasty"
	}
	procSetWindowTextW.Call(a.hwnd, uintptr(unsafe.Pointer(utf16(title))))
}

// ---- Suggestions and bookmarks ----------------------------------------------------------------

func (a *app) openTabSuggestions() []Suggestion {
	var out []Suggestion
	for _, t := range a.tabs {
		if t.kind == kindPage && t.url != "" {
			title := t.title
			if title == "" {
				title = t.url
			}
			out = append(out, Suggestion{Kind: "tab", Title: title, URL: t.url})
		}
	}
	return out
}

func (a *app) suggestions(q, mode string) []Suggestion {
	if mode == "switch" {
		var res []Suggestion
		lq := strings.ToLower(strings.TrimSpace(q))
		for _, s := range a.openTabSuggestions() {
			if strings.Contains(strings.ToLower(s.Title), lq) || strings.Contains(strings.ToLower(s.URL), lq) {
				res = append(res, s)
			}
		}
		return res
	}
	return Suggest(q, a.settings.SearchEngine, a.bookmarks, a.history, a.openTabSuggestions(), 7)
}

func (a *app) sendSuggestions(q, mode string) {
	if a.chrome == nil || !a.chromeReady {
		return
	}
	b, _ := json.Marshal(a.suggestions(q, mode))
	qq, _ := json.Marshal(strings.TrimSpace(q))
	a.chrome.Eval("fasty.suggestions(" + string(b) + "," + string(qq) + ")")
}

func (a *app) toggleBookmark() {
	t := a.active
	if t == nil || t.kind != kindPage || t.url == "" {
		return
	}
	a.bookmarks.Toggle(t.url, t.title)
	a.bookmarks.Flush()
	a.syncChrome()
}

// openVoice opens the new-tab page already listening: say the search, it runs when you stop.
func (a *app) openVoice() {
	a.showOverlay(overlayParams{Mode: "newtab", Voice: true, Private: a.active != nil && a.active.private})
}
