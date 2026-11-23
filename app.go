package main

// The application: one window, opened once; links from other apps arrive as
// tabs in it.

import (
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type App struct {
	*adw.Application
	settings    *Settings
	history     *History
	bookmarks   *Bookmarks
	curtain     *Curtain
	engine      *Engine
	passkeys    *passkeyStore
	authVault   *authVault
	shield      *Shield
	downloads   *Downloads
	extensions  *ExtensionManager
	memory      *gio.MemoryMonitor
	window      *Browser
	agentLn     net.Listener
	wantPrivate bool
}

func (a *App) zoomFor(u string) float64 {
	if z, ok := a.settings.SiteZoom[hostOf(u)]; ok && z > 0 {
		return z
	}
	return a.settings.Zoom
}

func newApp() *App {
	flags := gio.ApplicationHandlesOpen
	if os.Getenv("FASTY_SELFTEST") != "" {
		// A self-test runs on its own, beside a browser already open.
		flags |= gio.ApplicationNonUnique
	}
	a := &App{Application: adw.NewApplication(appID, flags), passkeys: passkeys()}
	glib.SetApplicationName("Fasty")
	glib.SetPrgname(appName)
	a.AddMainOption("private", 0, glib.OptionFlagNone, glib.OptionArgNone, "Open a private tab", "")
	a.ConnectStartup(a.startup)
	a.ConnectActivate(a.activate)
	a.ConnectOpen(a.open)
	a.ConnectHandleLocalOptions(func(opts *glib.VariantDict) int {
		if opts.Contains("private") {
			// Works whether this is the first launch or Fasty is already
			// running: the action is carried to the running instance.
			a.Register(nil)
			if a.IsRemote() {
				a.ActivateAction("private-tab", nil)
				return 0
			}
			a.wantPrivate = true
		}
		return -1
	})
	a.ConnectShutdown(a.flushAll)
	return a
}

func (a *App) startup() {
	a.settings = loadSettings()
	a.history = loadHistory()
	a.bookmarks = loadBookmarks()
	a.curtain = loadCurtain()
	a.engine = newEngine(a.settings)
	a.shield = newShield(a.settings)
	a.extensions = newExtensionManager(a.engine)
	a.downloads = &Downloads{settings: a.settings}
	a.downloads.watch(a.engine.session)
	// When the system runs short of memory, tabs in the background give
	// theirs back now instead of waiting for their idle time.
	a.memory = gio.MemoryMonitorDupDefault()
	a.memory.ConnectLowMemoryWarning(func(level gio.MemoryMonitorWarningLevel) {
		if a.window != nil {
			a.window.sleepForMemory(level)
		}
	})
	// Our own symbolic icons, whatever the desktop's theme lacks.
	gtk.SettingsGetDefault().SetObjectProperty("gtk-icon-theme-name", "Adwaita")
	css := gtk.NewCSSProvider()
	css.LoadFromString(asset("style.css"))
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), css, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
	p := gio.NewSimpleAction("private-tab", nil)
	p.ConnectActivate(func(*glib.Variant) {
		w, fresh := a.win()
		if fresh {
			w.restore(nil)
		}
		w.Present()
		w.newTab(true)
	})
	a.AddAction(p)
	q := gio.NewSimpleAction("quit", nil)
	q.ConnectActivate(func(*glib.Variant) { a.quitAll() })
	a.AddAction(q)
}

func (a *App) win() (*Browser, bool) {
	if a.window == nil {
		a.window = newBrowser(a)
		a.agentLn = startAgent(a.window)
		return a.window, true
	}
	return a.window, false
}

func (a *App) activate() {
	w, fresh := a.win()
	if fresh {
		w.restore(nil)
		if a.wantPrivate {
			w.newTab(true)
		}
		a.extensions.StartBackgrounds()
	}
	w.Present()
	if script := os.Getenv("FASTY_SELFTEST"); fresh && script != "" {
		runSelftest(w, script)
	}
}

func (a *App) open(files []gio.Filer, hint string) {
	var urls []string
	for _, f := range files {
		uri := f.URI()
		if p := f.Path(); strings.HasPrefix(uri, "file://") && p != "" {
			if _, err := os.Stat(p); err != nil {
				// Something typed on the command line that GIO took for a path.
				uri = resolve(filepath.Base(p), a.settings.Engine, a.settings.CustomEngine)
			}
		}
		urls = append(urls, uri)
	}
	w, fresh := a.win()
	if fresh {
		w.restore(urls)
	} else {
		for _, u := range urls {
			w.openTab(u, true, nil, false, "", false)
		}
	}
	w.Present()
	if script := os.Getenv("FASTY_SELFTEST"); fresh && script != "" {
		runSelftest(w, script)
	}
}

func (a *App) flushAll() {
	if a.settings == nil {
		return
	}
	stopAgent(a.agentLn)
	if a.extensions != nil {
		a.extensions.StopBackgrounds()
	}
	a.settings.flush()
	a.history.flush()
	a.bookmarks.flush()
	a.curtain.flush()
}

func (a *App) quitAll() {
	if a.window != nil {
		a.window.closing()
		a.window.Destroy()
	}
	a.Quit()
}

// pickRenderer chooses how GTK draws the window. On NVIDIA's driver its
// default, Vulkan, misses frames while a page scrolls and keeps the UI
// process busy (measured: about 20 of 240 frames dropped and 27 s of CPU in
// a 40 s scroll test, against 6 dropped and 3 s with OpenGL). GSK_RENDERER
// set by the user wins.
func pickRenderer() {
	if os.Getenv("GSK_RENDERER") != "" {
		return
	}
	if _, err := os.Stat("/proc/driver/nvidia"); err == nil {
		os.Setenv("GSK_RENDERER", "gl")
	}
}

// pickVideoDecoders steers video decoding to software while leaving WebKit's
// GPU compositing alone. YouTube changes the video source when an ad ends;
// hardware decoders (NVDEC, VA-API, VDPAU) can keep playing the new video's
// audio but stop handing frames to the compositor, leaving a black picture.
// Rank 0 demotes them below software decoders instead of disabling them, so a
// codec with no software decoder still plays. A rank set by the user wins.
//
// GPU decoding on NVIDIA is possible, as an option that is off by default:
// see gpudecode.go. (An earlier note here said it used more CPU; that
// measurement was wrong, the settings never reached the browser.)
func pickVideoDecoders() {
	if os.Getenv("GST_PLUGIN_FEATURE_RANK") != "" {
		return
	}
	hw := []string{
		"nvh264dec", "nvh265dec", "nvvp9dec", "nvav1dec", "nvvp8dec",
		"vaapih264dec", "vaapih264highdec", "vaapih265dec", "vaapivp9dec",
		"vaapiav1dec", "vaapivp8dec",
		"vdpauh264dec", "vdpauvp9dec", "vdpauav1dec",
	}
	ranks := make([]string, 0, len(hw))
	for _, f := range hw {
		ranks = append(ranks, f+":0")
	}
	os.Setenv("GST_PLUGIN_FEATURE_RANK", strings.Join(ranks, ","))
}

func main() {
	hardenFiles()
	gpuDecodeStart() // may restart the program once with a helper library loaded
	pickRenderer()
	pickVideoDecoders()
	os.Exit(newApp().Run(os.Args))
}
