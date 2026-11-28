//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// ---- Win32 plumbing ---------------------------------------------------------------------

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procShowWindow            = user32.NewProc("ShowWindow")
	procUpdateWindow          = user32.NewProc("UpdateWindow")
	procLoadCursorW           = user32.NewProc("LoadCursorW")
	procSetTimer              = user32.NewProc("SetTimer")
	procGetClientRect         = user32.NewProc("GetClientRect")
	procSetWindowTextW        = user32.NewProc("SetWindowTextW")
	procGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	procSetProcessDpiAwareCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procMessageBoxW           = user32.NewProc("MessageBoxW")
	procLoadImageW            = user32.NewProc("LoadImageW")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procIsIconic              = user32.NewProc("IsIconic")
	procGetTickCount64        = kernel32.NewProc("GetTickCount64")
	_                         = procGetTickCount64
	_                         = procSetWindowPos
	_                         = procIsIconic
	_                         = procLoadImageW
	_                         = procGetSystemMetrics
	_                         = procSetForegroundWindow
	_                         = procDestroyWindow
)

const (
	wmDestroy    = 0x0002
	wmSize       = 0x0005
	wmSetFocus   = 0x0007
	wmClose      = 0x0010
	wmTimer      = 0x0113
	wmDpiChanged = 0x02E0
	wmEmbed      = 0x8001 // WM_APP+1: create queued tab views

	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000
	swShowDefault      = 10
	idcArrow           = 32512

	timerReady = 1
	timerSleep = 2
	timerSync  = 3
	timerTest  = 4
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
	Private uint32
}

type winRect struct{ Left, Top, Right, Bottom int32 }

// closeController releases a WebView2 controller and the page process behind it. The edge
// package does not expose Close, so the call goes through the COM vtable: Close is the 24th
// slot (IUnknown's three, then the controller's methods in declaration order).
func closeController(c *edge.ICoreWebView2Controller) {
	if c == nil {
		return
	}
	vtbl := *(**[64]uintptr)(unsafe.Pointer(c))
	syscall.SyscallN(vtbl[24], uintptr(unsafe.Pointer(c)))
}

// ---- Browser state ----------------------------------------------------------------------

type tabKind int

const (
	kindPage tabKind = iota
	kindNewTab
	kindSettings
)

type tab struct {
	id         int
	kind       tabKind
	view       *edge.Chromium
	title      string
	url        string
	ready      bool
	pendingURL string
	asleep     bool
	lastActive time.Time
}

type app struct {
	hwnd        uintptr
	scale       float64
	dataDir     string
	webviewDir  string
	settings    Settings
	shield      *Shield
	chrome      *edge.Chromium
	chromeReady bool
	chromeLoad  bool
	tabs        []*tab
	active      *tab
	nextID      int
	focusAddr   bool
	quitting    bool
	lastBlocked int64
	selftest    *selfTest
	embedQueue  []*tab
}

var theApp *app

func (a *app) ensureDirs() error {
	if p := os.Getenv("FASTY_PROFILE"); p != "" { // set by the self-test so it never touches real data
		a.dataDir = filepath.Join(p, "data")
		a.webviewDir = filepath.Join(p, "WebView2")
	} else {
		base, err := os.UserConfigDir() // %AppData%
		if err != nil {
			return err
		}
		a.dataDir = filepath.Join(base, "FastyBrowser")
		local := os.Getenv("LocalAppData")
		if local == "" {
			local = base
		}
		a.webviewDir = filepath.Join(local, "FastyBrowser", "WebView2")
	}
	for _, d := range []string{a.dataDir, a.webviewDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func utf16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func (a *app) clientSize() (w, h int32) {
	var r winRect
	procGetClientRect.Call(a.hwnd, uintptr(unsafe.Pointer(&r)))
	return r.Right - r.Left, r.Bottom - r.Top
}

func (a *app) refreshScale() {
	dpi, _, _ := procGetDpiForWindow.Call(a.hwnd)
	if dpi == 0 {
		dpi = 96
	}
	a.scale = float64(dpi) / 96.0
}

// run builds the window, starts the first views and pumps messages until the window closes.
func run(startURLs []string, selftestBase string) error {
	a := &app{shield: NewShield(), scale: 1}
	theApp = a
	if err := a.ensureDirs(); err != nil {
		return err
	}
	a.settings = LoadSettings(a.dataDir)
	if selftestBase != "" {
		a.settings.ExtendedShield = false // no network in the self-test
		a.beginSelfTest(selftestBase)
	}
	// A GUI app has no console, so keep a small log next to the web view data.
	if f, err := os.Create(filepath.Join(a.webviewDir, "..", "fasty.log")); err == nil {
		log.SetOutput(f)
	}

	// Lighter background behaviour; Chromium flags that only reduce idle work.
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"--disable-background-networking --no-pings --disable-breakpad")

	procSetProcessDpiAwareCtx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)

	hInst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	cls := utf16("FastyBrowserWindow")
	wc := wndClassEx{
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  windows.Handle(hInst),
		Cursor:    windows.Handle(cursor),
		ClassName: cls,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(utf16("Fasty"))),
		wsOverlappedWindow, cwUseDefault, cwUseDefault, 1280, 820, 0, 0, hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	a.hwnd = hwnd
	a.refreshScale()
	log.Printf("window dpi scale %.2f", a.scale)
	procShowWindow.Call(hwnd, swShowDefault)
	procUpdateWindow.Call(hwnd)

	a.startChrome()
	a.restoreOrOpen(startURLs)
	a.loadExtendedShield()

	procSetTimer.Call(hwnd, timerReady, 25, 0)
	procSetTimer.Call(hwnd, timerSleep, 30_000, 0)
	procSetTimer.Call(hwnd, timerSync, 750, 0)
	if a.selftest != nil {
		procSetTimer.Call(hwnd, timerTest, 400, 0)
	}

	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	if a != nil && a.hwnd == hwnd {
		switch uint32(msg) {
		case wmSize:
			a.layout()
			return 0
		case wmDpiChanged:
			a.refreshScale()
			a.applyScale()
			a.layout()
			return 0
		case wmSetFocus:
			if a.active != nil && a.active.ready {
				a.active.view.Focus()
			}
			return 0
		case wmTimer:
			switch wparam {
			case timerReady:
				a.pollReady()
			case timerSleep:
				a.sleepIdleTabs()
			case timerSync:
				a.syncIfShieldChanged()
			case timerTest:
				a.selftestTick()
			}
			return 0
		case wmEmbed:
			a.embedPending()
			return 0
		case wmClose:
			a.saveSession()
			a.shutdown()
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (a *app) shutdown() {
	a.quitting = true
	for _, t := range a.tabs {
		if t.view != nil {
			t.view.ShuttingDown()
		}
	}
	if a.chrome != nil {
		a.chrome.ShuttingDown()
	}
}

// ---- Layout -----------------------------------------------------------------------------

func (a *app) chromeHeight() int32 { return int32(float64(ChromeHeightDIP) * a.scale) }

func (a *app) layout() {
	w, h := a.clientSize()
	_ = w
	ch := a.chromeHeight()
	if h < ch {
		h = ch
	}
	if a.chrome != nil && a.chromeReady {
		a.chrome.SetPadding(edge.Rect{Top: 0, Bottom: h - ch})
		a.chrome.Resize()
	}
	for _, t := range a.tabs {
		if t.ready {
			t.view.SetPadding(edge.Rect{Top: ch, Bottom: 0})
			t.view.Resize()
		}
	}
}

// applyScale is a placeholder for DPI changes. The edge package's PutRasterizationScale passes
// its float64 argument in an integer register, which Windows x64 ignores: calling it sets the
// scale to 0 and the whole interface renders at half size. WebView2 picks the monitor's scale
// itself when the view is created, so the app only has to resize its bounds (see layout).
func (a *app) applyScale() {}

// ---- Chrome (tab strip + toolbar) ---------------------------------------------------------

type chromeMsg struct {
	A  string  `json:"a"`
	V  string  `json:"v"`
	ID int     `json:"id"`
	D  int     `json:"d"`
	W  int     `json:"w"`
	H  int     `json:"h"`
	R  float64 `json:"r"`
}

func (a *app) startChrome() {
	v := edge.NewChromium()
	v.DataPath = a.webviewDir
	v.SetErrorCallback(func(err error) { log.Println("chrome view:", err) })
	v.MessageCallback = func(msg string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		a.onChromeMessage(msg)
	}
	a.chrome = v
	if !v.Embed(a.hwnd) {
		a.fatal("Could not start the WebView2 runtime. Install it from https://go.microsoft.com/fwlink/p/?LinkId=2124703 and try again.")
	}
}

func (a *app) onChromeMessage(raw string) {
	var m chromeMsg
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	switch m.A {
	case "ready":
		cw, chh := a.clientSize()
		log.Printf("chrome view: inner %dx%d css px, devicePixelRatio %.2f; window client %dx%d", m.W, m.H, m.R, cw, chh)
		a.syncChrome()
	case "sync":
		a.syncChrome()
	case "nav":
		a.navigate(a.active, m.V)
	case "back":
		a.evalActive("history.back()")
	case "fwd":
		a.evalActive("history.forward()")
	case "reload":
		a.reloadActive()
	case "newtab":
		a.newTab(kindNewTab, "")
	case "settings":
		a.openSettings()
	case "close":
		a.closeTab(a.byID(m.ID))
	case "closeactive":
		a.closeTab(a.active)
	case "switch":
		a.activate(a.byID(m.ID))
	case "cycle":
		a.cycle(m.D)
	}
}

type tabInfo struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Asleep bool   `json:"asleep"`
}

type chromeState struct {
	Tabs     []tabInfo `json:"tabs"`
	Active   int       `json:"active"`
	URL      string    `json:"url"`
	Blocked  int64     `json:"blocked"`
	ShieldOn bool      `json:"shieldOn"`
	Focus    bool      `json:"focusAddr"`
}

func (a *app) syncChrome() {
	if a.chrome == nil || !a.chromeReady || !a.chromeLoad {
		return
	}
	st := chromeState{Blocked: a.shield.Count(), ShieldOn: a.settings.Shield, Focus: a.focusAddr}
	for _, t := range a.tabs {
		title := t.title
		switch {
		case t.kind == kindNewTab:
			title = "New tab"
		case t.kind == kindSettings:
			title = "Settings"
		case title == "":
			title = DisplayURL(t.url)
		}
		st.Tabs = append(st.Tabs, tabInfo{ID: t.id, Title: title, Asleep: t.asleep})
	}
	if a.active != nil {
		st.Active = a.active.id
		if a.active.kind == kindPage {
			st.URL = DisplayURL(a.active.url)
		}
	}
	b, _ := json.Marshal(st)
	a.chrome.Eval("window.fasty&&window.fasty.update(" + string(b) + ")")
	if a.focusAddr {
		a.chrome.Focus()
		a.focusAddr = false
	}
	a.lastBlocked = st.Blocked
}

func (a *app) syncIfShieldChanged() {
	if a.shield.Count() != a.lastBlocked {
		a.syncChrome()
	}
}

func (a *app) setWindowTitle() {
	title := "Fasty"
	if a.active != nil {
		switch a.active.kind {
		case kindPage:
			if a.active.title != "" {
				title = a.active.title + " - Fasty"
			}
		case kindSettings:
			title = "Settings - Fasty"
		}
	}
	procSetWindowTextW.Call(a.hwnd, uintptr(unsafe.Pointer(utf16(title))))
}

// ---- Tabs ---------------------------------------------------------------------------------

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

func (a *app) restoreOrOpen(urls []string) {
	if len(urls) == 0 && a.settings.RestoreSession {
		if s, err := LoadSession(a.dataDir); err == nil {
			for i, u := range s.URLs {
				t := a.addTab(kindPage, u, -1)
				if i == s.Active {
					a.active = t
				}
			}
		}
	}
	for _, u := range urls {
		a.addTab(kindPage, ResolveInput(u, a.settings.SearchEngine), -1)
	}
	if len(a.tabs) == 0 {
		if a.settings.Homepage != "" {
			a.addTab(kindPage, a.settings.Homepage, -1)
		} else {
			a.addTab(kindNewTab, "", -1)
		}
	}
	if a.active == nil {
		a.active = a.tabs[0]
	}
	a.active.lastActive = time.Now()
	a.focusAddr = a.active.kind == kindNewTab
}

func (a *app) saveSession() {
	var s Session
	for _, t := range a.tabs {
		if t.kind == kindPage && t.url != "" && t.url != "about:blank" {
			if t == a.active {
				s.Active = len(s.URLs)
			}
			s.URLs = append(s.URLs, t.url)
		}
	}
	if len(s.URLs) > 0 {
		_ = SaveSession(a.dataDir, s)
	} else {
		_ = os.Remove(filepath.Join(a.dataDir, "session.json"))
	}
}

func (a *app) newTab(kind tabKind, url string) *tab {
	t := a.addTab(kind, url, -1)
	a.focusAddr = kind == kindNewTab
	a.activate(t)
	return t
}

// addTab creates the tab and its web view. index < 0 appends.
func (a *app) addTab(kind tabKind, url string, index int) *tab {
	a.nextID++
	t := &tab{id: a.nextID, kind: kind, url: url, pendingURL: url, lastActive: time.Now()}
	v := edge.NewChromium()
	v.DataPath = a.webviewDir
	v.SetErrorCallback(func(err error) { log.Println("tab view:", err) })
	v.MessageCallback = func(msg string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		a.onTabMessage(t, msg)
	}
	v.WebResourceRequestedCallback = func(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
		a.onResource(t, req, args)
	}
	v.ProcessFailedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2ProcessFailedEventArgs) {
		// The page process died; leave the tab as it is, a reload brings it back.
		log.Println("page process failed in tab", t.id)
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

func (a *app) embedPending() {
	q := a.embedQueue
	a.embedQueue = nil
	for _, t := range q {
		if a.indexOf(t) < 0 || a.quitting { // closed before its view was made
			continue
		}
		if !t.view.Embed(a.hwnd) {
			log.Println("could not create a page view for tab", t.id)
			a.closeTab(t)
		}
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
			t.view.Navigate(t.url)
		}
		if !a.focusAddr {
			t.view.Focus()
		}
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
	if t.view != nil {
		t.view.ShuttingDown()
		if t.ready {
			_ = t.view.GetController().PutIsVisible(false)
			closeController(t.view.GetController())
		}
	}
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if len(a.tabs) == 0 {
		a.newTab(kindNewTab, "")
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

func (a *app) openSettings() {
	for _, t := range a.tabs {
		if t.kind == kindSettings {
			a.activate(t)
			return
		}
	}
	a.newTab(kindSettings, "")
}

// pollReady finishes setting up views once WebView2 has created their controllers.
func (a *app) pollReady() {
	if a.chrome != nil && !a.chromeReady && a.chrome.GetController() != nil {
		a.chromeReady = true
		a.setupCommon(a.chrome)
		a.chrome.NavigateToString(chromeHTML)
		a.chromeLoad = true
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
	if c3 := v.GetController().GetICoreWebView2Controller3(); c3 != nil {
		scale, _ := c3.GetRasterizationScale()
		mode, _ := c3.GetBoundsMode()
		log.Printf("controller: rasterization scale %.2f, bounds mode %d (0 = raw pixels)", scale, mode)
	}
	if st, err := v.GetSettings(); err == nil {
		_ = st.PutIsWebMessageEnabled(true)
		_ = st.PutIsStatusBarEnabled(false)
	}
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
				v.Navigate(t.pendingURL)
			} else {
				t.asleep = true // restored background tabs stay asleep until opened
			}
		}
	case kindNewTab:
		v.NavigateToString(newTabHTML)
	case kindSettings:
		v.NavigateToString(settingsHTML(a.settings))
	}
	t.pendingURL = ""
	a.layout()
	if isActive && !a.focusAddr {
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
		if t == a.active || t.kind != kindPage || !t.ready || t.asleep || t.url == "" {
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

// ---- Page messages and navigation ---------------------------------------------------------

type pageMsg struct {
	A string    `json:"a"`
	T string    `json:"t"`
	U string    `json:"u"`
	K string    `json:"k"`
	V string    `json:"v"`
	S *Settings `json:"s"`
}

func (a *app) onTabMessage(t *tab, raw string) {
	var m pageMsg
	if json.Unmarshal([]byte(raw), &m) != nil || a.indexOf(t) < 0 {
		return
	}
	switch {
	case m.A == "meta" && t.kind == kindPage:
		if t.asleep || m.U == "about:blank" {
			return
		}
		t.title, t.url = m.T, m.U
		if t == a.active {
			a.setWindowTitle()
		}
		a.syncChrome()
	case m.A == "open" && t.kind == kindPage:
		if IsAddress(m.U) {
			a.newTab(kindPage, m.U)
		}
	case m.A == "key" && t.kind == kindPage:
		a.onShortcut(m.K)
	case m.A == "nav" && t.kind == kindNewTab:
		a.navigate(t, m.V)
	case m.A == "save" && t.kind == kindSettings && m.S != nil:
		a.settings = m.S.Normalize()
		if err := SaveSettings(a.dataDir, a.settings); err != nil {
			log.Println("save settings:", err)
		}
		a.syncChrome()
	}
}

func (a *app) onShortcut(k string) {
	switch k {
	case "t":
		a.newTab(kindNewTab, "")
	case "w":
		a.closeTab(a.active)
	case "l":
		a.focusAddr = true
		a.syncChrome()
	case "r":
		a.reloadActive()
	case "tab":
		a.cycle(1)
	case "shift-tab":
		a.cycle(-1)
	case "back":
		a.evalActive("history.back()")
	case "fwd":
		a.evalActive("history.forward()")
	}
}

func (a *app) evalActive(js string) {
	if t := a.active; t != nil && t.kind == kindPage && t.ready && !t.asleep {
		t.view.Eval(js)
	}
}

func (a *app) reloadActive() {
	t := a.active
	if t == nil || t.kind != kindPage || !t.ready {
		return
	}
	if t.asleep {
		t.asleep = false
		t.view.Navigate(t.url)
		a.syncChrome()
		return
	}
	t.view.Eval("location.reload()")
}

// navigate loads what was typed into the active tab. New-tab and settings tabs are
// replaced by a page tab in the same position, because only page tabs carry the page script.
func (a *app) navigate(t *tab, input string) {
	u := ResolveInput(input, a.settings.SearchEngine)
	if u == "" || t == nil {
		return
	}
	if t.kind != kindPage {
		idx := a.indexOf(t)
		nt := a.addTab(kindPage, u, idx+1)
		a.activate(nt)
		a.closeTab(t)
		return
	}
	t.url, t.title, t.asleep = u, "", false
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

func (a *app) fatal(msg string) {
	procMessageBoxW.Call(a.hwnd, uintptr(unsafe.Pointer(utf16(msg))), uintptr(unsafe.Pointer(utf16("Fasty"))), 0x10)
	os.Exit(1)
}
