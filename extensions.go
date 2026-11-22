package main

// Browser extensions: loads Chrome (MV2/MV3) and Firefox WebExtensions from
// disk, injects their content scripts, runs background pages in hidden views,
// handles .crx and .xpi installation, and talks to the Chrome Web Store and
// Firefox Add-ons for one-click installs.

import (
	"os/exec"
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
)

// --- manifest ----------------------------------------------------------------

type ExtManifest struct {
	ManifestVersion int               `json:"manifest_version"`
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Description     string            `json:"description"`
	DefaultLocale   string            `json:"default_locale"`
	Icons           map[string]string `json:"icons"`
	Permissions     []string          `json:"permissions"`
	OptPermissions  []string          `json:"optional_permissions"`
	HostPerms       []string          `json:"host_permissions"`
	OptHostPerms    []string          `json:"optional_host_permissions"`

	ContentScripts []ContentScriptDef `json:"content_scripts"`
	Background     *BackgroundDef     `json:"background"`

	// MV2
	BrowserAction *ActionDef `json:"browser_action"`
	PageAction    *ActionDef `json:"page_action"`
	OptionsPage   string     `json:"options_page"`

	// MV3
	Action    *ActionDef `json:"action"`
	OptionsUI *struct {
		Page string `json:"page"`
	} `json:"options_ui"`

	// Firefox
	Applications            *FirefoxApps `json:"applications"`
	BrowserSpecificSettings *FirefoxApps `json:"browser_specific_settings"`

	WebAccessibleResources json.RawMessage `json:"web_accessible_resources"`
	DNR                    json.RawMessage `json:"declarative_net_request"`
	Commands               map[string]struct {
		SuggestedKey map[string]string `json:"suggested_key"`
		Description  string            `json:"description"`
	} `json:"commands"`
	Externally             *struct {
		Matches []string `json:"matches"`
		IDs     []string `json:"ids"`
	} `json:"externally_connectable"`
}

type ContentScriptDef struct {
	Matches    []string `json:"matches"`
	ExcludeM   []string `json:"exclude_matches"`
	JS         []string `json:"js"`
	CSS        []string `json:"css"`
	RunAt      string   `json:"run_at"`
	AllFrames  bool     `json:"all_frames"`
	MatchAbout bool     `json:"match_about_blank"`
	World      string   `json:"world"`
	ID         string   `json:"id"`
	IncludeGlobs []string `json:"include_globs"`
	ExcludeGlobs []string `json:"exclude_globs"`

	JSCode     []string `json:"-"`
	CSSCode    []string `json:"-"`
	UserScript bool     `json:"-"`
}

type BackgroundDef struct {
	Scripts       []string `json:"scripts"`          // MV2
	Page          string   `json:"page"`             // MV2
	ServiceWorker string   `json:"service_worker"`   // MV3
	Type          string   `json:"type"`             // MV3: "module"
}

type ActionDef struct {
	DefaultPopup string            `json:"default_popup"`
	DefaultIcon  json.RawMessage   `json:"default_icon"`
	DefaultTitle string            `json:"default_title"`
}

type FirefoxApps struct {
	Gecko *struct {
		ID string `json:"id"`
	} `json:"gecko"`
}

// --- extension ---------------------------------------------------------------

type Extension struct {
	ID       string
	Dir      string
	Manifest ExtManifest
	Enabled  bool
	Source   string // "chrome" | "firefox" | "local"

	bgView *webkit.WebView
	bgUCM  *webkit.UserContentManager

	raw      json.RawMessage // manifest.json as written
	messages json.RawMessage // _locales messages for the default locale
	reason   string          // "install" or "update" until the background has been told
	logs     []string        // background console output, newest last
	badge    string
	title    string

	badgeColor    string
	iconOverride  string
	popupOverride string

	hostRes      []*regexp.Regexp
	dnrEnabled   map[string]bool
	dnrDynamic   []json.RawMessage
	dnrSession   []json.RawMessage
	dnrFilter    *webkit.UserContentFilter
	dnrGen       int
	dnrPending   bool
	dyn          []dynScript
	menus        []extMenu
	offscreen    *webkit.WebView
	offscreenUCM *webkit.UserContentManager
}

func (e *Extension) action() *ActionDef {
	if e.Manifest.Action != nil {
		return e.Manifest.Action
	}
	if e.Manifest.BrowserAction != nil {
		return e.Manifest.BrowserAction
	}
	return e.Manifest.PageAction
}

func (e *Extension) optionsPage() string {
	if e.Manifest.OptionsUI != nil && e.Manifest.OptionsUI.Page != "" {
		return e.Manifest.OptionsUI.Page
	}
	return e.Manifest.OptionsPage
}

func (e *Extension) firefoxID() string {
	if e.Manifest.BrowserSpecificSettings != nil && e.Manifest.BrowserSpecificSettings.Gecko != nil {
		return e.Manifest.BrowserSpecificSettings.Gecko.ID
	}
	if e.Manifest.Applications != nil && e.Manifest.Applications.Gecko != nil {
		return e.Manifest.Applications.Gecko.ID
	}
	return ""
}

// realID is the id the extension itself knows: the gecko id for Firefox add-ons.
func (e *Extension) realID() string {
	if id := e.firefoxID(); id != "" {
		return id
	}
	return e.ID
}

func (e *Extension) readFile(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(e.Dir, rel))
}

// --- extension manager -------------------------------------------------------

type ExtensionManager struct {
	dir        string
	extensions []*Extension
	enabled    map[string]bool // id -> enabled
	engine     *Engine
	apiShim    string
	storeShim  string
	listeners  []func()

	retired  []any
	popupView *webkit.WebView
	natives  map[string]*nativeConn
	auths    []*authFlow
	helpers  map[string]*fetchHelper
	dnrStore *webkit.UserContentFilterStore
	captures map[string]*exec.Cmd
	b        *Browser
	secret   string
	reqSeq   int
	pending  map[int]func(json.RawMessage, string)
	ports    map[string]*extPort
	session  map[string]map[string]json.RawMessage // storage.session, memory only
}

func newExtensionManager(engine *Engine) *ExtensionManager {
	dir := filepath.Join(dataDir(), "extensions")
	os.MkdirAll(dir, 0o755)
	m := &ExtensionManager{
		dir:       dir,
		enabled:   map[string]bool{},
		engine:    engine,
		apiShim:   asset("js/webext.js"),
		storeShim: asset("js/ext_store.js"),
		secret:    randomHex(16),
		pending:   map[int]func(json.RawMessage, string){},
		ports:     map[string]*extPort{},
		session:   map[string]map[string]json.RawMessage{},
		captures:  map[string]*exec.Cmd{},
		helpers:   map[string]*fetchHelper{},
		natives:   map[string]*nativeConn{},
	}
	registerExtScheme(engine, dir)
	theExt = m
	readJSON("extensions_enabled.json", &m.enabled)
	m.scan()
	for _, e := range m.extensions {
		m.dnrRebuild(e)
	}
	return m
}

func (m *ExtensionManager) scan() {
	m.extensions = nil
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if want := sanitizeID(e.Name()); want != e.Name() {
			if _, err := os.Stat(filepath.Join(m.dir, want)); err != nil &&
				os.Rename(filepath.Join(m.dir, e.Name()), filepath.Join(m.dir, want)) == nil {
				if en, ok := m.enabled[e.Name()]; ok {
					m.enabled[want] = en
					delete(m.enabled, e.Name())
				}
				m.saveEnabled()
				m.scan()
				return
			}
		}
		ext := m.loadOne(filepath.Join(m.dir, e.Name()))
		if ext != nil {
			m.extensions = append(m.extensions, ext)
		}
	}
	sort.Slice(m.extensions, func(i, j int) bool {
		return strings.ToLower(m.extensions[i].Manifest.Name) < strings.ToLower(m.extensions[j].Manifest.Name)
	})
}

func (m *ExtensionManager) loadOne(dir string) *Extension {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var mf ExtManifest
	if json.Unmarshal(data, &mf) != nil || mf.Name == "" {
		return nil
	}
	id := filepath.Base(dir)
	ext := &Extension{
		ID:       id,
		Dir:      dir,
		Manifest: mf,
		raw:      json.RawMessage(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))),
	}
	ext.loadMessages()
	if ext.firefoxID() != "" {
		ext.Source = "firefox"
	} else {
		ext.Source = "chrome"
	}
	if en, ok := m.enabled[id]; ok {
		ext.Enabled = en
	} else {
		ext.Enabled = true
	}
	m.dnrInit(ext)
	m.loadDyn(ext)
	return ext
}

func (m *ExtensionManager) saveEnabled() {
	for _, e := range m.extensions {
		m.enabled[e.ID] = e.Enabled
	}
	writeJSON("extensions_enabled.json", m.enabled)
}

func (m *ExtensionManager) find(id string) *Extension {
	for _, e := range m.extensions {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (m *ExtensionManager) enabledList() []*Extension {
	var out []*Extension
	for _, e := range m.extensions {
		if e.Enabled {
			out = append(out, e)
		}
	}
	return out
}

func (m *ExtensionManager) tell() {
	for _, fn := range m.listeners {
		fn()
	}
}

// --- content script injection ------------------------------------------------

// matchPatternToRegex converts a WebExtension match pattern to a regexp.
// Patterns: <scheme>://<host>/<path>
// <all_urls> matches http and https.
func matchPatternToRegex(pattern string) string {
	if pattern == "<all_urls>" {
		return `^https?://.*$`
	}
	parts := strings.SplitN(pattern, "://", 2)
	if len(parts) != 2 {
		return ""
	}
	scheme := parts[0]
	rest := parts[1]
	hostPath := strings.SplitN(rest, "/", 2)
	host := hostPath[0]
	path := ""
	if len(hostPath) > 1 {
		path = hostPath[1]
	}

	var re strings.Builder
	re.WriteString("^")

	switch scheme {
	case "*":
		re.WriteString("https?")
	case "http", "https", "file", "ftp":
		re.WriteString(regexp.QuoteMeta(scheme))
	default:
		return ""
	}
	re.WriteString("://")

	if host == "*" {
		re.WriteString("[^/]*")
	} else if strings.HasPrefix(host, "*.") {
		re.WriteString("([^/]+\\.")
		re.WriteString(regexp.QuoteMeta(host[2:]))
		re.WriteString("|")
		re.WriteString(regexp.QuoteMeta(host[2:]))
		re.WriteString(")")
	} else {
		re.WriteString(regexp.QuoteMeta(host))
	}

	re.WriteString("(:[0-9]+)?/")
	for _, c := range path {
		if c == '*' {
			re.WriteString(".*")
		} else {
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString("$")
	return re.String()
}

// matchPatternsToAllowList converts match patterns to WebKit allow-list format.
func matchPatternsToAllowList(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if p == "<all_urls>" {
			return nil // nil means all URLs in WebKit
		}
		// WebKit uses shell-like patterns: *://host/*
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// --- .crx / .xpi installation -----------------------------------------------

// InstallFromFile installs an extension from a .crx or .xpi file.
func (m *ExtensionManager) InstallFromFile(path string, id ...string) (string, error) {
	pid := ""
	if len(id) > 0 {
		pid = id[0]
	}
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".crx"):
		return m.installCRX(path, pid)
	case strings.HasSuffix(lower, ".xpi"):
		return m.installXPI(path, pid)
	case strings.HasSuffix(lower, ".zip"):
		return m.installZIP(path, pid)
	default:
		return "", fmt.Errorf("unknown extension format: %s", filepath.Ext(path))
	}
}

func (m *ExtensionManager) installCRX(path, pid string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// CRX3 format: magic "Cr24" + version(4) + header_len(4) + header + zip
	if len(data) < 12 || string(data[:4]) != "Cr24" {
		// Maybe it's just a zip
		return m.installZIPData(data, pid)
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	if version == 3 {
		headerLen := binary.LittleEndian.Uint32(data[8:12])
		zipStart := 12 + headerLen
		if uint32(len(data)) < zipStart {
			return "", fmt.Errorf("crx header exceeds file size")
		}
		return m.installZIPData(data[zipStart:], pid)
	}
	// CRX2: magic + version(4) + pubkey_len(4) + sig_len(4) + pubkey + sig + zip
	if version == 2 && len(data) >= 16 {
		pkLen := binary.LittleEndian.Uint32(data[8:12])
		sigLen := binary.LittleEndian.Uint32(data[12:16])
		zipStart := 16 + pkLen + sigLen
		if uint32(len(data)) < zipStart {
			return "", fmt.Errorf("crx2 header exceeds file size")
		}
		return m.installZIPData(data[zipStart:], pid)
	}
	return "", fmt.Errorf("unknown crx version %d", version)
}

func (m *ExtensionManager) installXPI(path, pid string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return m.installZIPData(data, pid)
}

func (m *ExtensionManager) installZIP(path, pid string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return m.installZIPData(data, pid)
}

func (m *ExtensionManager) installZIPData(data []byte, pid string) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("not a valid zip: %v", err)
	}

	// Find manifest.json to determine extension ID.
	var manifest []byte
	prefix := ""
	for _, f := range r.File {
		name := f.Name
		if filepath.Base(name) == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			manifest, _ = io.ReadAll(rc)
			rc.Close()
			if dir := filepath.Dir(name); dir != "." {
				prefix = dir + "/"
			}
			break
		}
	}
	if manifest == nil {
		return "", fmt.Errorf("no manifest.json found in archive")
	}

	manifest = bytes.TrimPrefix(manifest, []byte("\xef\xbb\xbf"))
	var mf ExtManifest
	if err := json.Unmarshal(manifest, &mf); err != nil {
		return "", fmt.Errorf("bad manifest.json: %v", err)
	}

	// Determine ID: Firefox gecko id, or use the name slug.
	id := pid
	if id == "" && mf.BrowserSpecificSettings != nil && mf.BrowserSpecificSettings.Gecko != nil {
		id = mf.BrowserSpecificSettings.Gecko.ID
	} else if id == "" && mf.Applications != nil && mf.Applications.Gecko != nil {
		id = mf.Applications.Gecko.ID
	}
	if id == "" {
		id = slugify(zipLocalized(r, prefix, &mf, mf.Name))
		if id == "" {
			id = fmt.Sprintf("ext-%d", len(m.extensions))
		}
	}
	// Sanitize id for filesystem.
	id = sanitizeID(id)

	destDir := filepath.Join(m.dir, id)
	os.RemoveAll(destDir)
	os.MkdirAll(destDir, 0o755)

	for _, f := range r.File {
		name := f.Name
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		if name == "" || strings.HasSuffix(name, "/") {
			os.MkdirAll(filepath.Join(destDir, name), 0o755)
			continue
		}
		target := filepath.Join(destDir, name)
		os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			continue
		}
		io.Copy(out, rc)
		out.Close()
		rc.Close()
	}

	// Reload
	ext := m.loadOne(destDir)
	if ext == nil {
		os.RemoveAll(destDir)
		return "", fmt.Errorf("installed but could not load extension")
	}
	ext.reason = "install"
	for i, e := range m.extensions {
		if e.ID == ext.ID {
			ext.reason = "update"
			m.stopBackground(e)
			m.closeOffscreen(e)
			m.dnrApply(e, nil)
			m.extensions = append(m.extensions[:i], m.extensions[i+1:]...)
			break
		}
	}
	m.extensions = append(m.extensions, ext)
	sort.Slice(m.extensions, func(i, j int) bool {
		return strings.ToLower(m.extensions[i].Manifest.Name) < strings.ToLower(m.extensions[j].Manifest.Name)
	})
	m.saveEnabled()
	m.dnrRebuild(ext)
	if ext.Enabled {
		fresh := ext.reason == "install"
		m.startBackground(ext)
		if fresh {
			m.injectIntoOpenTabs(ext)
		}
	}
	m.tell()
	return ext.Manifest.Name, nil
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == ' ' || c == '-' || c == '_':
			b.WriteRune('-')
		}
	}
	r := b.String()
	if len(r) > 60 {
		r = r[:60]
	}
	return r
}

// sanitizeID makes an id safe as a URL host and directory name: lowercase
// letters, digits and "-_." only ("@" would read as a user-info separator).
func sanitizeID(id string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(id) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// Remove uninstalls an extension.
func (m *ExtensionManager) Remove(id string) {
	for i, e := range m.extensions {
		if e.ID == id {
			m.stopBackground(e)
			m.closeOffscreen(e)
			m.dnrApply(e, nil)
			m.extensions = append(m.extensions[:i], m.extensions[i+1:]...)
			break
		}
	}
	os.RemoveAll(filepath.Join(m.dir, id))
	delete(m.enabled, id)
	m.saveEnabled()
	m.tell()
}

// Toggle enables or disables an extension.
func (m *ExtensionManager) Toggle(id string) {
	if e := m.find(id); e != nil {
		e.Enabled = !e.Enabled
		if e.Enabled {
			m.startBackground(e)
		} else {
			m.stopBackground(e)
			m.closeOffscreen(e)
		}
		m.dnrRebuild(e)
		m.saveEnabled()
		m.tell()
	}
}

// --- download interception for extension installs ----------------------------

// IsExtensionDownload checks if a download URL is an extension file and
// handles it.
func (m *ExtensionManager) IsExtensionDownload(uri, suggested string) bool {
	lower := strings.ToLower(suggested)
	if strings.HasSuffix(lower, ".crx") || strings.HasSuffix(lower, ".xpi") {
		return true
	}
	if strings.Contains(uri, "clients2.google.com/service/update2/crx") ||
		strings.Contains(uri, "addons.mozilla.org") && strings.Contains(uri, "/downloads/") {
		return true
	}
	return false
}

// InstallDownloaded installs an extension that was downloaded to path.
func (m *ExtensionManager) InstallDownloaded(path string, onDone func(name string, err error)) {
	name, err := m.InstallFromFile(path)
	onDone(name, err)
}

// --- Chrome Web Store CRX download URL ---------------------------------------

func chromeStoreDownloadURL(extensionID string) string {
	return fmt.Sprintf(
		"https://clients2.google.com/service/update2/crx?response=redirect&acceptformat=crx2,crx3&prodversion=130.0&x=id%%3D%s%%26installsource%%3Dondemand%%26uc",
		extensionID)
}

// HandleStoreInstall processes an install request from the injected store
// helper script.
func (m *ExtensionManager) HandleStoreInstall(body string, session *webkit.NetworkSession, onDone func(name string, err error)) {
	var req struct {
		Store string `json:"store"`
		ExtID string `json:"extId"`
		URL   string `json:"url"`
	}
	if json.Unmarshal([]byte(body), &req) != nil {
		return
	}
	switch req.Store {
	case "chrome":
		if chromeIDRe.FindString(req.ExtID) != req.ExtID {
			return
		}
		m.installDownloaded(chromeStoreDownloadURL(req.ExtID), ".crx", req.ExtID, onDone)
	case "firefox":
		if !strings.HasPrefix(req.URL, "https://addons.mozilla.org/") {
			return
		}
		m.installDownloaded(req.URL, ".xpi", req.ExtID, onDone)
	}
}

// downloadFile fetches a URL to a local file using net/http.
func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// zipLocalized resolves a "__MSG_key__" name using the archive's own
// _locales, so the install id comes from the real name.
func zipLocalized(r *zip.Reader, prefix string, mf *ExtManifest, s string) string {
	if !strings.HasPrefix(s, "__MSG_") || !strings.HasSuffix(s, "__") {
		return s
	}
	locales := []string{"en", "en_US", "en_GB"}
	if mf.DefaultLocale != "" {
		locales = append([]string{mf.DefaultLocale}, locales...)
	}
	for _, l := range locales {
		for _, f := range r.File {
			if f.Name != prefix+"_locales/"+l+"/messages.json" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			e := &Extension{messages: bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))}
			return e.localize(s)
		}
	}
	return s
}
