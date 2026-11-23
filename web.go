package main

// WebKit: the sessions pages run in, the ad blocker, and the scripts every
// page is given (Shield.swift, YouTubeAds.swift).

/*
#cgo pkg-config: webkitgtk-6.0
#include <stdlib.h>
#include <webkit/webkit.h>

// sb_pressure is how much memory a WebKit process may hold before it starts
// throwing caches away (conservative), then everything it can (strict); at
// the limit WebKit stops the page.
static WebKitMemoryPressureSettings *sb_pressure(guint limit_mb, double conservative, double strict) {
	WebKitMemoryPressureSettings *m = webkit_memory_pressure_settings_new();
	webkit_memory_pressure_settings_set_memory_limit(m, limit_mb);
	webkit_memory_pressure_settings_set_conservative_threshold(m, conservative);
	webkit_memory_pressure_settings_set_strict_threshold(m, strict);
	webkit_memory_pressure_settings_set_poll_interval(m, 15);
	return m;
}

// sb_context is the web context every page runs in, with a memory budget for
// its processes. The network process gets a smaller one; it must be set
// before any session exists.
static gpointer sb_context(guint page_mb, guint net_mb) {
	WebKitMemoryPressureSettings *net = sb_pressure(net_mb, 0.33, 0.5);
	webkit_network_session_set_memory_pressure_settings(net);
	webkit_memory_pressure_settings_free(net);
	WebKitMemoryPressureSettings *page = sb_pressure(page_mb, 0.2, 0.35);
	gpointer ctx = g_object_new(WEBKIT_TYPE_WEB_CONTEXT, "memory-pressure-settings", page, NULL);
	webkit_memory_pressure_settings_free(page);
	return ctx;
}

// sb_features turns on WebKit features by identifier; unknown ones are
// skipped, so a newer or older WebKit doesn't break anything.
static void sb_features(gpointer settings, char **ids, int n) {
	WebKitFeatureList *all = webkit_settings_get_all_features();
	for (int i = 0; i < n; i++) {
		WebKitFeature *f = webkit_feature_list_find(all, ids[i]);
		if (f != NULL)
			webkit_settings_set_feature_enabled(WEBKIT_SETTINGS(settings), f, TRUE);
	}
	webkit_feature_list_unref(all);
}

static GtkWidget *sb_view(gpointer ctx, gpointer session, gpointer ucm, gpointer settings) {
	return GTK_WIDGET(g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context", ctx, "network-session", session,
		"user-content-manager", ucm, "settings", settings, NULL));
}

static GtkWidget *sb_related_view(gpointer related, gpointer ucm, gpointer settings) {
	// A related view inherits the opener's network session (cookies, storage)
	// automatically. Passing network-session as well makes WebKit refuse it
	// with "network-session property can't be set when related-view is set".
	return GTK_WIDGET(g_object_new(WEBKIT_TYPE_WEB_VIEW, "related-view", related,
		"user-content-manager", ucm, "settings", settings, NULL));
}
// sb_shot saves what a widget last drew as a PNG, for the self-test. It uses
// the picture its parent keeps from the last frame (asking a widget to draw
// again on demand often gives nothing), cropped to where the widget is. A
// window is shot through its content.
static gboolean sb_shot(GtkWidget *w, const char *path) {
	if (GTK_IS_WINDOW(w) && gtk_window_get_child(GTK_WINDOW(w)) != NULL)
		w = gtk_window_get_child(GTK_WINDOW(w));
	GtkWidget *parent = gtk_widget_get_parent(w);
	graphene_rect_t rect;
	if (parent == NULL || !gtk_widget_compute_bounds(w, parent, &rect)) {
		g_warning("shot: widget isn't placed");
		return FALSE;
	}
	GtkSnapshot *snap = gtk_snapshot_new();
	gtk_widget_snapshot_child(parent, w, snap);
	GskRenderNode *node = gtk_snapshot_free_to_node(snap);
	if (node == NULL) { g_warning("shot: no render node"); return FALSE; }
	GskRenderer *r = gtk_native_get_renderer(gtk_widget_get_native(w));
	GdkTexture *tex = gsk_renderer_render_texture(r, node, &rect);
	gsk_render_node_unref(node);
	if (tex == NULL) { g_warning("shot: no texture"); return FALSE; }
	gboolean ok = gdk_texture_save_to_png(tex, path);
	if (!ok) g_warning("shot: saving failed");
	g_object_unref(tex);
	return ok;
}
*/
import "C"

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

//go:embed assets
var assets embed.FS

func asset(name string) string {
	b, err := assets.ReadFile("assets/" + name)
	if err != nil {
		log.Printf("fasty: missing asset %s", name)
	}
	return string(b)
}

func native(o coreglib.Objector) C.gpointer {
	return C.gpointer(unsafe.Pointer(coreglib.InternObject(o).Native()))
}

func wrapView(p *C.GtkWidget) *webkit.WebView {
	obj := coreglib.Take(unsafe.Pointer(p))
	return &webkit.WebView{WebViewBase: webkit.WebViewBase{Widget: gtk.Widget{
		InitiallyUnowned: coreglib.InitiallyUnowned{Object: obj},
		Object:           obj,
		Accessible:       gtk.Accessible{Object: obj},
		Buildable:        gtk.Buildable{Object: obj},
		ConstraintTarget: gtk.ConstraintTarget{Object: obj},
	}}}
}

// --- Sessions ------------------------------------------------------------------

// Engine holds the persistent session everyone shares, makes ephemeral ones
// for private tabs, and the settings every page gets.
type Engine struct {
	settings    *Settings
	session     *webkit.NetworkSession
	favicons    *webkit.FaviconDatabase
	webSettings *webkit.Settings
	picker      string
	wheel       string
	youtube     string
	pipButton   string
	compat      string
	webauthn    string
	ctx         C.gpointer
}

// context is the web context pages run in, as a Go object.
func (e *Engine) context() *webkit.WebContext {
	obj := coreglib.Take(unsafe.Pointer(e.ctx))
	return &webkit.WebContext{Object: obj}
}

// memoryBudget sizes WebKit's limits to the machine, in MB: a page process
// starts dropping caches at a fifth of its limit, so on 16 GB that is about
// 500 MB instead of WebKit's default of 1 GB, and is stopped only at the
// limit itself. The network process holds caches and cookies, not pages.
func memoryBudget() (page, net uint) {
	var kb uint
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		fmt.Sscanf(string(b), "MemTotal: %d kB", &kb)
	}
	page = max(1024, min(kb/1024/6, 2560))
	return page, page / 2
}

func newEngine(s *Settings) *Engine {
	e := &Engine{settings: s, picker: asset("js/picker.js"), wheel: asset("js/wheel.js"),
		youtube: asset("js/youtube.js"), pipButton: asset("js/pipbutton.js"), compat: asset("js/compat.js"), webauthn: asset("js/webauthn.js")}
	page, net := memoryBudget()
	e.ctx = C.sb_context(C.guint(page), C.guint(net))
	base := filepath.Join(dataDir(), "web")
	cache := filepath.Join(cacheDir(), "web")
	e.session = webkit.NewNetworkSession(base, cache)
	e.session.SetItpEnabled(s.TrackingPrevention)
	e.session.SetPersistentCredentialStorageEnabled(true)
	mgr := e.session.WebsiteDataManager()
	mgr.SetFaviconsEnabled(true)
	e.favicons = mgr.FaviconDatabase()
	cookies := e.session.CookieManager()
	cookies.SetPersistentStorage(filepath.Join(base, "cookies.sqlite"), webkit.CookiePersistentStorageSqlite)
	if s.TrackingPrevention {
		cookies.SetAcceptPolicy(webkit.CookiePolicyAcceptNoThirdParty)
	} else {
		cookies.SetAcceptPolicy(webkit.CookiePolicyAcceptAlways)
	}
	ctx := e.context()
	ctx.SetSpellCheckingEnabled(s.Spellcheck)
	var langs []string
	for _, l := range glib.GetLanguageNames() {
		if !strings.Contains(l, ".") && l != "C" && len(langs) < 2 {
			langs = append(langs, l)
		}
	}
	if len(langs) == 0 {
		langs = []string{"en_US"}
	}
	ctx.SetSpellCheckingLanguages(langs)
	ctx.SetCacheModel(webkit.CacheModelWebBrowser)
	e.webSettings = e.makeSettings()
	return e
}

func (e *Engine) makeSettings() *webkit.Settings {
	s := webkit.NewSettings()
	s.SetEnableDeveloperExtras(true)
	s.SetJavascriptCanOpenWindowsAutomatically(true)
	s.SetEnableSmoothScrolling(e.settings.SmoothScrolling)
	s.SetEnableBackForwardNavigationGestures(true)
	s.SetEnableMedia(true)
	s.SetEnableMediaStream(true)
	s.SetEnableWebrtc(true)
	s.SetEnableEncryptedMedia(true)
	s.SetEnableWebgl(true)
	s.SetEnableWebaudio(true)
	s.SetEnableHtml5Database(true)
	s.SetEnableHtml5LocalStorage(true)
	// The back/forward cache keeps whole previous pages alive. It held a
	// YouTube page's 500 MB after the tab had moved on to a 270 MB shop, so a
	// page you leave is let go; Back reloads it from the HTTP cache.
	s.SetEnablePageCache(false)
	s.SetEnableFullscreen(true)
	s.SetEnableSiteSpecificQuirks(true)
	s.SetMediaPlaybackRequiresUserGesture(false)
	enableFeatures(s, modernWeb)
	if e.settings.HardwareAcceleration {
		s.SetHardwareAccelerationPolicy(webkit.HardwareAccelerationPolicyAlways)
	} else {
		s.SetHardwareAccelerationPolicy(webkit.HardwareAccelerationPolicyNever)
	}
	return s
}

// modernWeb are the web platform features this WebKit has but keeps off by
// default, picked from the ones that are finished or about to ship in Safari
// and already ship in another browser. Half-built ones (appearance: base,
// the Painting API) stay off.
var modernWeb = []string{
	// CSS
	"CSSLineClamp",                         // line-clamp
	"CSSCornerShape",                       // corner-shape: squircle, bevel…
	"CSSScrollStateContainerQueries",       // @container scroll-state(stuck: top)
	"CSSOverflowClipMargin",                // overflow-clip-margin
	"CSSTextJustify",                       // text-justify
	"CSSWordBreakAutoPhrase",               // word-break: auto-phrase
	"CSSDProperty",                         // the SVG d property in CSS
	"CSSFontFaceMetricOverrideDescriptors", // ascent-override, size-adjust…
	// HTML and DOM
	"SwitchControl",              // <input type=checkbox switch>
	"InputTypeColorEnhancements", // alpha and colorspace on color inputs
	"RequestIdleCallback",        // requestIdleCallback
	"CanvasColorSpace",           // display-p3 canvases
	"StorageAPI", "StorageAPIEstimate",
	"ScreenOrientationAPI",
	"AltitudeAngle", "AzimuthAngle", // pen angles on pointer events
	"EnableElementCurrentCSSZoom",
	// JavaScript
	"ExplicitResourceManagement", // using / await using
	"IteratorIncludes", "IteratorJoin", "IteratorChunking",
}

func enableFeatures(s *webkit.Settings, ids []string) {
	cs := make([]*C.char, len(ids))
	for i, id := range ids {
		cs[i] = C.CString(id)
		defer C.free(unsafe.Pointer(cs[i]))
	}
	C.sb_features(native(s), &cs[0], C.int(len(cs)))
}

func (e *Engine) privateSession() *webkit.NetworkSession {
	s := webkit.NewNetworkSessionEphemeral()
	s.SetItpEnabled(true)
	return s
}

func (e *Engine) contentManager() *webkit.UserContentManager {
	return e.contentManagerWithExtensions(nil)
}

func (e *Engine) contentManagerWithExtensions(ext *ExtensionManager) *webkit.UserContentManager {
	ucm := webkit.NewUserContentManager()
	ucm.AddScript(webkit.NewUserScript(e.picker, webkit.UserContentInjectTopFrame,
		webkit.UserScriptInjectAtDocumentStart, nil, nil))
	ucm.RegisterScriptMessageHandler("officeVeil", "")
	// Strip YouTube's ad data before its player sees it. Must run in the
	// page's own world so it can replace fetch/XHR and the inline response.
	ucm.AddScript(webkit.NewUserScript(e.youtube, webkit.UserContentInjectTopFrame,
		webkit.UserScriptInjectAtDocumentStart,
		[]string{"*://*.youtube.com/*", "*://*.youtube-nocookie.com/*"}, nil))
	// Fixes for sites that expect things WebKitGTK doesn't have.
	ucm.AddScript(webkit.NewUserScript(e.compat, webkit.UserContentInjectTopFrame,
		webkit.UserScriptInjectAtDocumentStart,
		[]string{"https://github.com/*"}, nil))
	// Passkeys: the standard WebAuthn API, backed by webauthn.go.
	ucm.AddScript(webkit.NewUserScript(e.webauthn, webkit.UserContentInjectTopFrame,
		webkit.UserScriptInjectAtDocumentStart,
		[]string{"https://*/*", "http://localhost/*", "http://*.localhost/*"}, nil))
	// The hover "Picture in Picture" button on videos.
	ucm.AddScript(webkit.NewUserScript(e.pipButton, webkit.UserContentInjectAllFrames,
		webkit.UserScriptInjectAtDocumentStart, nil, nil))
	if e.settings.SmoothScrolling {
		ucm.AddScript(webkit.NewUserScript(e.wheel, webkit.UserContentInjectAllFrames,
			webkit.UserScriptInjectAtDocumentStart, nil, nil))
	}
	// Extension content scripts and store helpers.
	if ext != nil {
		ext.InjectExtensions(ucm)
		ext.injectWebConnect(ucm)
	}
	return ucm
}

func (e *Engine) newView(session *webkit.NetworkSession, ucm *webkit.UserContentManager) *webkit.WebView {
	return wrapView(C.sb_view(e.ctx, native(session), native(ucm), native(e.webSettings)))
}

func (e *Engine) relatedView(related *webkit.WebView, ucm *webkit.UserContentManager) *webkit.WebView {
	return wrapView(C.sb_related_view(native(related), native(ucm), native(e.webSettings)))
}

// veilSheets puts every site's hidden elements on, each sheet allowed only on
// its own site.
func veilSheets(ucm *webkit.UserContentManager, c *Curtain) {
	ucm.RemoveAllStyleSheets()
	for _, host := range c.Hosts() {
		css := c.CSS(host)
		if css == "" {
			continue
		}
		allow := []string{"*://" + host + "/*", "*://*." + host + "/*"}
		ucm.AddStyleSheet(webkit.NewUserStyleSheet(css, webkit.UserContentInjectTopFrame,
			webkit.UserStyleLevelUser, allow, nil))
	}
}

func openExternal(uri string) {
	if err := gio.AppInfoLaunchDefaultForURI(uri, nil); err != nil {
		log.Printf("fasty: couldn't open %s: %v", uri, err)
	}
}

func shotPNG(w gtk.Widgetter, path string) bool {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	return C.sb_shot((*C.GtkWidget)(unsafe.Pointer(coreglib.InternObject(w).Native())), cs) != 0
}
