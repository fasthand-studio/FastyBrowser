//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// ---- Who is asking: a tab's own page or the overlay ---------------------------------------------

// sink is where answers go. Internal pages (tabs) and the overlay both talk to the app with the
// same calls, so they share one handler.
type sink struct {
	tab     *tab
	eval    func(js string)
	alive   func() bool
	private bool
}

func (a *app) tabSink(t *tab) *sink {
	return &sink{
		tab:     t,
		private: t.private,
		eval:    func(js string) { t.view.Eval(js) },
		alive:   func() bool { return a.indexOf(t) >= 0 && t.ready },
	}
}

func (a *app) overlaySink() *sink {
	o := a.ov
	return &sink{
		private: o.private,
		eval:    func(js string) { o.view.Eval(js) },
		alive:   func() bool { return o.ready },
	}
}

// ---- The glass overlay --------------------------------------------------------------------------

// The overlay is one extra web view that covers the whole window. Spotlight and Settings are drawn
// in it over a blurred snapshot of what was on screen, which is how the page seems to blur behind
// a translucent card: WebView2 views cannot be see-through, so the snapshot stands in for it.
type overlay struct {
	view    *edge.Chromium
	hwnd    uintptr
	ready   bool
	shown   bool
	mode    string
	private bool
	seq     int
}

var (
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procPrintWindow         = user32.NewProc("PrintWindow")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes, Bits  uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// captureClient takes a picture of the window's client area as it looks now.
func (a *app) captureClient() []byte {
	w, h := a.clientSize()
	if w < 8 || h < 8 {
		return nil
	}
	hdc, _, _ := procGetDC.Call(a.hwnd)
	if hdc == 0 {
		return nil
	}
	defer procReleaseDC.Call(a.hwnd, hdc)
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatibleBmp.Call(hdc, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer procSelectObject.Call(mem, old)
	procPrintWindow.Call(a.hwnd, mem, 3) // PW_CLIENTONLY | PW_RENDERFULLCONTENT: includes the web views

	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: w, Height: -h, Planes: 1, Bits: 32}
	buf := make([]byte, int(w)*int(h)*4)
	if r, _, _ := procGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); r == 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(buf); i += 4 { // BGRA -> RGBA, opaque
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, img, &jpeg.Options{Quality: 78}) != nil {
		return nil
	}
	return out.Bytes()
}

func (a *app) startOverlay() {
	v := edge.NewChromium()
	v.DataPath = a.webviewDir
	v.SetErrorCallback(func(err error) { log.Println("overlay view:", err) })
	v.MessageCallback = func(msg string, _ *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		if src, _ := args.GetSource(); ui.IsInternal(src) {
			a.onOverlayMessage(msg)
		}
	}
	a.ov = &overlay{view: v}
	h, ok := a.embedView(v)
	if !ok {
		log.Println("could not create the overlay view")
		return
	}
	a.ov.hwnd = h
}

// setupOverlay runs once the overlay's controller exists: load its page, keep it hidden.
func (a *app) setupOverlay() {
	o := a.ov
	if o == nil || o.ready || o.view.GetController() == nil {
		return
	}
	a.setupCommon(o.view)
	o.view.SetPermission(edge.CoreWebView2PermissionKindMicrophone, edge.CoreWebView2PermissionStateAllow)
	_ = o.view.GetController().PutIsVisible(false)
	q := "overlay.html?t=" + a.themeName()
	if a.selftest != nil {
		q += "&selftest=1" // the page reports what it shows, and skips the real recogniser
	}
	o.view.Navigate(ui.URL(q))
	o.ready = true
	if m := os.Getenv("FASTY_DEV_OVERLAY"); m != "" { // developer aid: open the overlay a few seconds after start
		time.AfterFunc(6*time.Second, func() {
			a.runOnUI(func() {
				switch m {
				case "settings":
					a.openSettings(os.Getenv("FASTY_DEV_SECTION"))
				case "voice":
					a.showOverlay(overlayParams{Mode: "newtab", Voice: true})
				default:
					a.showOverlay(overlayParams{Mode: "newtab", URL: os.Getenv("FASTY_DEV_TEXT")})
				}
			})
		})
	}
}

type overlayParams struct {
	Mode    string `json:"mode"` // spotlight modes: newtab | address | switch; or settings
	Bg      string `json:"bg"`
	Theme   string `json:"theme"`
	URL     string `json:"url"`
	Private bool   `json:"private"`
	Voice   bool   `json:"voice"`
	Section string `json:"section"`
}

// showOverlay snapshots the window and shows the overlay over it.
func (a *app) showOverlay(p overlayParams) {
	o := a.ov
	if o == nil || !o.ready {
		return
	}
	if o.shown { // already open: just switch what it shows, keeping the first snapshot
		p.Bg = ""
	} else {
		if jpg := a.captureClient(); jpg != nil {
			o.seq++
			name := strconv.Itoa(o.seq) + ".jpg"
			ui.DropImages()
			ui.SetImage(name, jpg)
			p.Bg = "bg/" + name
		}
	}
	o.mode, o.private = p.Mode, p.Private
	p.Theme = a.themeName()
	if p.Mode == "address" && p.URL == "" && a.active != nil && a.active.kind == kindPage {
		p.URL = DisplayURL(a.active.url)
	}
	o.shown = true
	a.layout()
	_ = o.view.GetController().PutIsVisible(true)
	a.raiseOverlay()
	b, _ := json.Marshal(p)
	o.view.Eval("window.fastyOverlay&&window.fastyOverlay(" + string(b) + ")")
	o.view.Focus()
}

func (a *app) hideOverlay() {
	o := a.ov
	if o == nil || !o.shown {
		return
	}
	o.shown = false
	a.stopVoice()
	o.view.Eval("window.fastyOverlayHide&&window.fastyOverlayHide()")
	_ = o.view.GetController().PutIsVisible(false)
	ui.DropImages()
	a.layout()
	if a.active != nil && a.active.ready {
		a.active.view.Focus()
	}
}

func (a *app) raiseOverlay() {
	if a.ov != nil && a.ov.hwnd != 0 {
		procSetWindowPos.Call(a.ov.hwnd, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
}

type overlayMsg struct {
	A string `json:"a"`
}

func (a *app) onOverlayMessage(raw string) {
	var head overlayMsg
	if json.Unmarshal([]byte(raw), &head) != nil {
		return
	}
	switch head.A {
	case "close":
		a.hideOverlay()
	case "rpc":
		var m pageMsg
		if json.Unmarshal([]byte(raw), &m) == nil {
			a.handleRPC(a.overlaySink(), m)
		}
	case "key":
		var m pageMsg
		if json.Unmarshal([]byte(raw), &m) == nil && a.ov.mode != "settings" {
			// keys the browser owns still work while Spotlight is open, e.g. Ctrl+W closes nothing here
			_ = m
		}
	}
}

// rpcNav is what a Spotlight search does with its result: open it in a new tab, the current
// tab, or switch to an open tab, depending on how Spotlight was opened.
func (a *app) rpcNav(s *sink, input, mode string, private bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}
	if s.tab != nil { // a tab's own page asked (the blank page's links)
		a.navigate(s.tab, input)
		return
	}
	a.hideOverlay()
	switch mode {
	case "newtab":
		u := CleanURL(ResolveInput(input, a.settings.SearchEngine), a.settings.CleanLinks && IsAddress(input))
		if u != "" {
			a.newTab(kindPage, u, private)
		}
	case "switch":
		for _, t := range a.tabs {
			if t.kind == kindPage && (t.url == input || strings.EqualFold(t.title, input)) {
				a.activate(t)
				return
			}
		}
		a.navigate(a.active, input)
	default: // "address"
		a.navigate(a.active, input)
	}
}

// openSpotlight is Ctrl+T, Ctrl+L, Ctrl+K and a click on the address field.
func (a *app) openSpotlight(mode string, voice bool) {
	private := a.active != nil && a.active.private
	if mode == "newtab" && private {
		private = true
	}
	a.showOverlay(overlayParams{Mode: mode, Private: private, Voice: voice})
}

func (a *app) openSettings(section string) {
	a.showOverlay(overlayParams{Mode: "settings", Section: section})
}

var _ = windows.Handle(0)
