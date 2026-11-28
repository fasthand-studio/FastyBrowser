//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ---- Win32 plumbing ---------------------------------------------------------------------

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostQuitMessage    = user32.NewProc("PostQuitMessage")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procUpdateWindow       = user32.NewProc("UpdateWindow")
	procLoadCursorW        = user32.NewProc("LoadCursorW")
	procLoadImageW         = user32.NewProc("LoadImageW")
	procSetTimer           = user32.NewProc("SetTimer")
	procGetClientRect      = user32.NewProc("GetClientRect")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procSetWindowTextW     = user32.NewProc("SetWindowTextW")
	procGetDpiForWindow    = user32.NewProc("GetDpiForWindow")
	procMessageBoxW        = user32.NewProc("MessageBoxW")
	procInvalidateRect     = user32.NewProc("InvalidateRect")
	procFillRect           = user32.NewProc("FillRect")
	procFrameRect          = user32.NewProc("FrameRect")
	procSetWindowLongPtrW  = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW  = user32.NewProc("GetWindowLongPtrW")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procMonitorFromWindow  = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	procEnumChildWindows   = user32.NewProc("EnumChildWindows")
	procGetParent          = user32.NewProc("GetParent")
	procSetForegroundWnd   = user32.NewProc("SetForegroundWindow")
	procOpenClipboard      = user32.NewProc("OpenClipboard")
	procCloseClipboard     = user32.NewProc("CloseClipboard")
	procEmptyClipboard     = user32.NewProc("EmptyClipboard")
	procSetClipboardData   = user32.NewProc("SetClipboardData")
	procGetClipboardData   = user32.NewProc("GetClipboardData")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc        = kernel32.NewProc("GlobalAlloc")
	procGlobalLock         = kernel32.NewProc("GlobalLock")
	procGlobalUnlock       = kernel32.NewProc("GlobalUnlock")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDwmSetWindowAttr   = dwmapi.NewProc("DwmSetWindowAttribute")
	procShellExecuteW      = shell32.NewProc("ShellExecuteW")
	procExtractIconW       = shell32.NewProc("ExtractIconW")
	procSetProcessDpiAware = user32.NewProc("SetProcessDpiAwarenessContext")
)

const (
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmEraseBkgnd    = 0x0014
	wmSettingChange = 0x001A
	wmTimer         = 0x0113
	wmDpiChanged    = 0x02E0
	wmEmbed         = 0x8001 // WM_APP+1: create queued tab views
	wmRun           = 0x8002 // WM_APP+2: run tasks queued by other goroutines

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000
	cwUseDefault       = 0x80000000
	swShowDefault      = 10
	idcArrow           = 32512
	gwlStyle           = ^uintptr(15) // -16
	swpNoMove          = 0x2
	swpNoSize          = 0x1
	swpNoActivate      = 0x10
	swpFrameChanged    = 0x20
	swpNoZOrder        = 0x4
	hwndTop            = 0
	imageIcon          = 1
	lrDefaultSize      = 0x40
	lrShared           = 0x8000

	timerReady = 1
	timerSleep = 2
	timerSync  = 3
	timerTest  = 4

	dwmUseDarkMode   = 20
	dwmCaptionColor  = 35
	dwmTextColor     = 36
	dwmBorderColor   = 34
	cfUnicodeText    = 13
	gmemMoveable     = 0x2
	monitorDefaultNr = 2
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

type monitorInfo struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
}

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

func utf16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func rgb(hex uint32) uintptr { // 0xRRGGBB -> COLORREF 0x00BBGGRR
	return uintptr(((hex & 0xff) << 16) | (hex & 0xff00) | ((hex >> 16) & 0xff))
}

// ---- Theme ------------------------------------------------------------------------------

type palette struct {
	dark                 bool
	frame, hairline, ink uint32
}

var (
	lightPalette = palette{false, 0xf4f4f4, 0xe8e8e8, 0x171717}
	darkPalette  = palette{true, 0x131313, 0x333333, 0xededed}
)

func systemIsDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

func (a *app) pal() palette {
	switch a.settings.Appearance {
	case "dark":
		return darkPalette
	case "light":
		return lightPalette
	}
	if systemIsDark() {
		return darkPalette
	}
	return lightPalette
}

func (a *app) themeName() string {
	if a.pal().dark {
		return "dark"
	}
	return "light"
}

// applyTheme paints the title bar and the area around the page in the Linux build's colours.
func (a *app) applyTheme() {
	p := a.pal()
	dark := uint32(0)
	if p.dark {
		dark = 1
	}
	procDwmSetWindowAttr.Call(a.hwnd, dwmUseDarkMode, uintptr(unsafe.Pointer(&dark)), 4)
	for _, x := range []struct {
		attr uintptr
		c    uint32
	}{{dwmCaptionColor, p.frame}, {dwmBorderColor, p.frame}, {dwmTextColor, p.ink}} {
		c := uint32(rgb(x.c))
		procDwmSetWindowAttr.Call(a.hwnd, x.attr, uintptr(unsafe.Pointer(&c)), 4) // ignored before Windows 11
	}
	for _, b := range []*uintptr{&a.brushFrame, &a.brushHair} {
		if *b != 0 {
			procDeleteObject.Call(*b)
		}
	}
	a.brushFrame, _, _ = procCreateSolidBrush.Call(rgb(p.frame))
	a.brushHair, _, _ = procCreateSolidBrush.Call(rgb(p.hairline))
	procInvalidateRect.Call(a.hwnd, 0, 1)
	a.pushTheme()
}

func (a *app) pushTheme() {
	th := a.themeName()
	js := "document.documentElement.dataset.theme='" + th + "'"
	if a.chrome != nil && a.chromeReady {
		a.chrome.Eval(js)
	}
	for _, t := range a.tabs {
		if t.kind == kindInternal && t.ready {
			t.view.Eval(js)
		}
	}
}

// ---- Browser state ----------------------------------------------------------------------

type tabKind int

const (
	kindPage     tabKind = iota
	kindInternal         // one of the browser's own pages, see ui/ (page name in tab.page)
)

type tab struct {
	id         int
	kind       tabKind
	page       string
	view       *edge.Chromium
	hwnd       uintptr
	title      string
	url        string
	favicon    string
	loading    bool
	canBack    bool
	canFwd     bool
	ready      bool
	pendingURL string
	asleep     bool
	pinned     bool
	private    bool
	privDir    string
	reading    bool
	recorded   string // last address written to history
	lastActive time.Time
}

type closedTab struct {
	url    string
	pinned bool
}

type app struct {
	hwnd        uintptr
	scale       float64
	dataDir     string
	webviewDir  string
	settings    Settings
	shield      *Shield
	bookmarks   *Bookmarks
	history     *History
	chrome      *edge.Chromium
	chromeHwnd  uintptr
	chromeReady bool
	tabs        []*tab
	active      *tab
	nextID      int
	quitting    bool
	lastBlocked int64
	selftest    *selfTest
	embedQueue  []*tab
	voice       *voiceSession
	closed      []closedTab
	brushFrame  uintptr
	brushHair   uintptr
	overlayDIP  int
	sideLive    int // sidebar width while the user is dragging it
	fullscreen  bool
	savedStyle  uintptr
	savedRect   winRect
	lastTheme   string
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

func (a *app) px(dip int) int32 { return int32(float64(dip)*a.scale + 0.5) }

// run builds the window, starts the first views and pumps messages until the window closes.
func run(startURLs []string, selftestBase string) error {
	a := &app{shield: NewShield(), scale: 1}
	theApp = a
	if err := a.ensureDirs(); err != nil {
		return err
	}
	a.settings = LoadSettings(a.dataDir)
	a.bookmarks = LoadBookmarks(a.dataDir)
	a.history = LoadHistory(a.dataDir)
	var uerr error
	if ui, uerr = startUIServer(); uerr != nil {
		return uerr
	}
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

	procSetProcessDpiAware.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)

	hInst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	exe, _ := os.Executable()
	icon, _, _ := procExtractIconW.Call(hInst, uintptr(unsafe.Pointer(utf16(exe))), 0)
	cls := utf16("FastyBrowserWindow")
	wc := wndClassEx{
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  windows.Handle(hInst),
		Cursor:    windows.Handle(cursor),
		Icon:      windows.Handle(icon),
		IconSm:    windows.Handle(icon),
		ClassName: cls,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	w, h := 1280, 820
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(utf16("Fasty"))),
		wsOverlappedWindow, cwUseDefault, cwUseDefault, uintptr(w), uintptr(h), 0, 0, hInst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	a.hwnd = hwnd
	a.refreshScale()
	a.applyTheme()
	log.Printf("window dpi scale %.2f, theme %s", a.scale, a.themeName())
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
		case wmEraseBkgnd:
			a.paintBackground(wparam)
			return 1
		case wmSize:
			a.layout()
			return 0
		case wmDpiChanged:
			a.refreshScale()
			a.layout()
			return 0
		case wmSettingChange:
			if a.settings.Appearance == "system" && a.lastTheme != a.themeName() {
				a.lastTheme = a.themeName()
				a.applyTheme()
			}
			return 0
		case wmSetFocus:
			if a.active != nil && a.active.ready {
				a.active.view.Focus()
			}
			return 0
		case wmEmbed:
			a.embedPending()
			return 0
		case wmRun:
			a.drainUI()
			return 0
		case wmTimer:
			switch wparam {
			case timerReady:
				a.pollReady()
			case timerSleep:
				a.sleepIdleTabs()
			case timerSync:
				a.syncIfShieldChanged()
				a.flushStores()
			case timerTest:
				a.selftestTick()
			}
			return 0
		case wmClose:
			a.saveSession()
			a.flushStores()
			a.shutdown()
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (a *app) flushStores() {
	a.bookmarks.Flush()
	a.history.Flush()
}

func (a *app) shutdown() {
	a.quitting = true
	a.stopVoice()
	for _, t := range a.tabs {
		if t.view != nil {
			t.view.ShuttingDown()
		}
		a.removePrivateData(t)
	}
	if a.chrome != nil {
		a.chrome.ShuttingDown()
	}
}

// ---- Layout -----------------------------------------------------------------------------

const (
	topBarDIP = 84 // tab chips row + address row in the top layout
	cardGap   = 8  // space around the page, like the Linux .page-card margin
)

type layoutRects struct {
	chrome, page winRect
	chromeShown  bool
}

// sidebarWidth is the sidebar's width in device pixels, 0 when folded or in the top layout.
func (a *app) sidebarWidth() int32 {
	if a.settings.TabsSide != "left" || a.settings.SidebarFolded {
		return 0
	}
	w := a.settings.SidebarWidth
	if a.sideLive > 0 {
		w = a.sideLive
	}
	return a.px(w)
}

func (a *app) rects() layoutRects {
	w, h := a.clientSize()
	g := a.px(cardGap)
	var r layoutRects
	if a.fullscreen {
		r.page = winRect{0, 0, w, h}
		return r
	}
	r.chromeShown = true
	if a.settings.TabsSide == "top" {
		th := a.px(topBarDIP)
		if o := a.px(a.overlayDIP); o > th {
			th = o
		}
		if th > h {
			th = h
		}
		r.chrome = winRect{0, 0, w, th}
		r.page = winRect{g, a.px(topBarDIP), w - g, h - g}
		return r
	}
	sw := a.sidebarWidth()
	left := g
	if sw > 0 {
		left = sw
		r.chrome = winRect{0, 0, sw, h}
	} else if a.settings.SidebarFolded {
		r.chromeShown = false
	}
	r.page = winRect{left, g, w - g, h - g}
	return r
}

func (a *app) layout() {
	if a.hwnd == 0 {
		return
	}
	w, h := a.clientSize()
	r := a.rects()
	if a.chrome != nil && a.chromeReady {
		_ = a.chrome.GetController().PutIsVisible(r.chromeShown)
		a.chrome.SetPadding(edge.Rect{Left: r.chrome.Left, Top: r.chrome.Top, Right: w - r.chrome.Right, Bottom: h - r.chrome.Bottom})
		a.chrome.Resize()
	}
	for _, t := range a.tabs {
		if t.ready {
			t.view.SetPadding(edge.Rect{Left: r.page.Left, Top: r.page.Top, Right: w - r.page.Right, Bottom: h - r.page.Bottom})
			t.view.Resize()
		}
	}
	procInvalidateRect.Call(a.hwnd, 0, 1)
	a.raiseChrome()
}

// paintBackground draws the frame colour and the one-pixel hairline around the page card.
func (a *app) paintBackground(hdc uintptr) {
	var cr winRect
	procGetClientRect.Call(a.hwnd, uintptr(unsafe.Pointer(&cr)))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&cr)), a.brushFrame)
	if a.fullscreen {
		return
	}
	pr := a.rects().page
	pr.Left--
	pr.Top--
	pr.Right++
	pr.Bottom++
	procFrameRect.Call(hdc, uintptr(unsafe.Pointer(&pr)), a.brushHair)
}

// raiseChrome keeps the sidebar/toolbar view above the page views, so menus and suggestions that
// grow it over the page stay visible.
func (a *app) raiseChrome() {
	if a.chromeHwnd != 0 {
		procSetWindowPos.Call(a.chromeHwnd, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
}

// childWindows lists the direct child windows of the main window.
func (a *app) childWindows() map[uintptr]bool {
	set := map[uintptr]bool{}
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		if p, _, _ := procGetParent.Call(h); p == a.hwnd {
			set[h] = true
		}
		return 1
	})
	procEnumChildWindows.Call(a.hwnd, cb, 0)
	return set
}

// embedView creates a WebView2 view in the main window and remembers its window handle.
func (a *app) embedView(v *edge.Chromium) (uintptr, bool) {
	before := a.childWindows()
	if !v.Embed(a.hwnd) {
		return 0, false
	}
	var found uintptr
	for h := range a.childWindows() {
		if !before[h] {
			found = h
		}
	}
	return found, true
}

func (a *app) setFullscreen(on bool) {
	if on == a.fullscreen {
		return
	}
	if on {
		a.savedStyle, _, _ = procGetWindowLongPtrW.Call(a.hwnd, gwlStyle)
		procGetWindowRect.Call(a.hwnd, uintptr(unsafe.Pointer(&a.savedRect)))
		mon, _, _ := procMonitorFromWindow.Call(a.hwnd, monitorDefaultNr)
		mi := monitorInfo{}
		mi.Size = uint32(unsafe.Sizeof(mi))
		procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
		a.fullscreen = true
		procSetWindowLongPtrW.Call(a.hwnd, gwlStyle, wsPopup|wsVisible)
		procSetWindowPos.Call(a.hwnd, hwndTop, uintptr(mi.Monitor.Left), uintptr(mi.Monitor.Top),
			uintptr(mi.Monitor.Right-mi.Monitor.Left), uintptr(mi.Monitor.Bottom-mi.Monitor.Top), swpFrameChanged)
	} else {
		a.fullscreen = false
		procSetWindowLongPtrW.Call(a.hwnd, gwlStyle, a.savedStyle)
		r := a.savedRect
		procSetWindowPos.Call(a.hwnd, hwndTop, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpFrameChanged)
	}
	a.layout()
}

func (a *app) fatal(msg string) {
	procMessageBoxW.Call(a.hwnd, uintptr(unsafe.Pointer(utf16(msg))), uintptr(unsafe.Pointer(utf16("Fasty"))), 0x10)
	os.Exit(1)
}

// ---- Clipboard and shell ------------------------------------------------------------------

// ptrOf turns an address returned by the Windows API (GlobalLock) into a pointer.
func ptrOf(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func clipboardSet(text string) {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return
	}
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	n := uintptr(len(u) * 2)
	hm, _, _ := procGlobalAlloc.Call(gmemMoveable, n)
	if hm == 0 {
		return
	}
	p, _, _ := procGlobalLock.Call(hm)
	if p == 0 {
		return
	}
	copy(unsafe.Slice((*uint16)(ptrOf(p)), len(u)), u)
	procGlobalUnlock.Call(hm)
	procSetClipboardData.Call(cfUnicodeText, hm)
}

func clipboardGet() string {
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return ""
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	return windows.UTF16PtrToString((*uint16)(ptrOf(p)))
}

func shellOpen(target string) {
	procShellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(target))), 0, 0, 1)
}
