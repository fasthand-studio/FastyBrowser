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
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
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

// --- Shield ------------------------------------------------------------------

var unwanted = []string{
	"doubleclick.net", "googlesyndication.com", "googleadservices.com",
	"googletagservices.com", "google-analytics.com", "googletagmanager.com",
	"adservice.google.com", "amazon-adsystem.com", "adnxs.com", "adsrvr.org",
	"criteo.com", "criteo.net", "taboola.com", "outbrain.com",
	"rubiconproject.com", "pubmatic.com", "openx.net", "casalemedia.com",
	"smartadserver.com", "sharethrough.com", "indexww.com", "bidswitch.net",
	"33across.com", "teads.tv", "moatads.com", "adroll.com",
	"scorecardresearch.com", "quantserve.com", "chartbeat.com",
	"hotjar.com", "mouseflow.com", "fullstory.com", "clarity.ms",
	"mixpanel.com", "amplitude.com", "segment.com", "segment.io",
	"branch.io", "appsflyer.com", "adjust.com", "analytics.tiktok.com",
	"connect.facebook.net", "ads-twitter.com", "analytics.twitter.com",
}

var slots = []string{
	".adsbygoogle", "ins.adsbygoogle", `[id^="google_ads_"]`,
	`[id^="div-gpt-ad"]`, `[id^="taboola-"]`, "#taboola-below-article",
	`iframe[src*="doubleclick.net"]`, `iframe[src*="googlesyndication"]`,
	`iframe[src*="amazon-adsystem"]`,
	".ad-slot", ".ad-slot-container", ".top-banner-ad-container",
	".ad-leaderboard", ".ad-billboard", ".ad-giga", ".ad-mpu", ".ad-mrec",
	".ad-unit", ".adunit", ".adslot", ".dfp-ad", ".gpt-ad", ".w_ad",
}

var slotsBySite = []struct {
	sites    []string
	selector string
}{
	{[]string{"*as.com", "*elpais.com"}, ".ad"},
	{[]string{"*theguardian.com"}, ".top-fronts-banner-ad-container"},
	{[]string{"*independent.co.uk", "*the-independent.com"}, "#billboard-wrapper"},
	{[]string{"*cnn.com"}, ".ad-slot-header__wrapper"},
}

var youtubePaths = []string{
	"/api/stats/ads", "/pagead/", "/ptracking", "/get_midroll_info",
	"/youtubei/v1/player/ad_break", "/pcs/activeview",
}

func networkFilter(domain string) string {
	return `^https?://([^/]+\.)?` + strings.ReplaceAll(domain, ".", `\.`) + `[:/]`
}

type rule map[string]any

func shieldRules() []rule {
	var rules []rule
	for _, d := range unwanted {
		rules = append(rules, rule{"trigger": rule{"url-filter": networkFilter(d), "load-type": []string{"third-party"}},
			"action": rule{"type": "block"}})
	}
	rules = append(rules, rule{"trigger": rule{"url-filter": ".*"},
		"action": rule{"type": "css-display-none", "selector": strings.Join(slots, ", ")}})
	for _, p := range youtubePaths {
		rules = append(rules, rule{"trigger": rule{"url-filter": `^https?://([^/]+\.)?youtube\.com` + strings.ReplaceAll(p, ".", `\.`)},
			"action": rule{"type": "block"}})
	}
	rules = append(rules, rule{"trigger": rule{"url-filter": `^https?://([^/]+\.)?youtube\.com/api/stats/qoe\?.*adformat=`},
		"action": rule{"type": "block"}})
	for _, f := range []string{`^https?://([^/]+\.)?google\.com/pagead/`, `^https?://imasdk\.googleapis\.com/`} {
		rules = append(rules, rule{"trigger": rule{"url-filter": f, "if-domain": []string{"*youtube.com", "*youtube-nocookie.com"}},
			"action": rule{"type": "block"}})
	}
	for _, s := range slotsBySite {
		rules = append(rules, rule{"trigger": rule{"url-filter": ".*", "if-domain": s.sites},
			"action": rule{"type": "css-display-none", "selector": s.selector}})
	}
	return rules
}

type tuneReq struct {
	ucm  *webkit.UserContentManager
	host string
}

// Shield is a content filter compiled once and enforced inside WebKit's
// networking, before a request is made.
type Shield struct {
	settings *Settings
	store    *webkit.UserContentFilterStore
	filter   *webkit.UserContentFilter
	ident    string
	waiting  []tuneReq
	holding  map[*webkit.UserContentManager]bool
}

func newShield(s *Settings) *Shield {
	folder := filepath.Join(cacheDir(), "filters")
	os.MkdirAll(folder, 0o755)
	sh := &Shield{settings: s, store: webkit.NewUserContentFilterStore(folder),
		holding: map[*webkit.UserContentManager]bool{}}
	sh.compile()
	return sh
}

func (sh *Shield) compile() {
	data, _ := json.Marshal(shieldRules())
	sum := sha256.Sum256(data)
	sh.ident = "office-shield-" + hex.EncodeToString(sum[:])[:32]
	sh.store.Load(context.Background(), sh.ident, func(res gio.AsyncResulter) {
		f, err := sh.store.LoadFinish(res)
		if err == nil && f != nil {
			sh.ready(f)
			return
		}
		sh.store.Save(context.Background(), sh.ident, glib.NewBytes(data), func(res gio.AsyncResulter) {
			f, err := sh.store.SaveFinish(res)
			if err != nil {
				log.Printf("fasty: shield: %v", err)
				return
			}
			sh.ready(f)
		})
	})
}

func (sh *Shield) ready(f *webkit.UserContentFilter) {
	sh.filter = f
	for _, w := range sh.waiting {
		sh.Tune(w.ucm, w.host, true)
	}
	sh.waiting = nil
	sh.store.FetchIdentifiers(context.Background(), func(res gio.AsyncResulter) {
		for _, old := range sh.store.FetchIdentifiersFinish(res) {
			if strings.HasPrefix(old, "office-shield") && old != sh.ident {
				sh.store.Remove(context.Background(), old, nil)
			}
		}
	})
}

func (sh *Shield) Paused(host string) bool {
	if host == "" {
		return false
	}
	for _, h := range sh.settings.ShieldPaused {
		if h == host {
			return true
		}
	}
	return false
}

func (sh *Shield) Pause(host string, off bool) {
	sh.settings.Set("shield_paused", func(d *SettingsData) {
		var keep []string
		for _, h := range d.ShieldPaused {
			if h != host {
				keep = append(keep, h)
			}
		}
		if off {
			keep = append(keep, host)
		}
		if keep == nil {
			keep = []string{}
		}
		d.ShieldPaused = keep
	})
}

// Tune puts the filter on a page's content manager, or takes it off, for the
// site it is about to show.
func (sh *Shield) Tune(ucm *webkit.UserContentManager, host string, again bool) {
	if ucm == nil {
		return
	}
	if sh.filter == nil {
		sh.waiting = append(sh.waiting, tuneReq{ucm, host})
		return
	}
	on := sh.settings.Shield && !sh.Paused(host)
	if was, ok := sh.holding[ucm]; ok && was == on && !again {
		return
	}
	ucm.RemoveFilter(sh.filter)
	if on {
		ucm.AddFilter(sh.filter)
	}
	sh.holding[ucm] = on
}

func (sh *Shield) Forget(ucm *webkit.UserContentManager) { delete(sh.holding, ucm) }

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
	e := &Engine{settings: s, picker: asset("js/picker.js"), wheel: asset("js/wheel.js")}
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
	s.SetEnablePageCache(true)
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
	ucm := webkit.NewUserContentManager()
	ucm.AddScript(webkit.NewUserScript(e.picker, webkit.UserContentInjectTopFrame,
		webkit.UserScriptInjectAtDocumentStart, nil, nil))
	ucm.RegisterScriptMessageHandler("officeVeil", "")
	if e.settings.SmoothScrolling {
		ucm.AddScript(webkit.NewUserScript(e.wheel, webkit.UserContentInjectAllFrames,
			webkit.UserScriptInjectAtDocumentStart, nil, nil))
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
