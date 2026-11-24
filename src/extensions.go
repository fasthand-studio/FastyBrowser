package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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
	Externally *struct {
		Matches []string `json:"matches"`
		IDs     []string `json:"ids"`
	} `json:"externally_connectable"`
}

type ContentScriptDef struct {
	Matches      []string `json:"matches"`
	ExcludeM     []string `json:"exclude_matches"`
	JS           []string `json:"js"`
	CSS          []string `json:"css"`
	RunAt        string   `json:"run_at"`
	AllFrames    bool     `json:"all_frames"`
	MatchAbout   bool     `json:"match_about_blank"`
	World        string   `json:"world"`
	ID           string   `json:"id"`
	IncludeGlobs []string `json:"include_globs"`
	ExcludeGlobs []string `json:"exclude_globs"`

	JSCode     []string `json:"-"`
	CSSCode    []string `json:"-"`
	UserScript bool     `json:"-"`
}

type BackgroundDef struct {
	Scripts       []string `json:"scripts"`        // MV2
	Page          string   `json:"page"`           // MV2
	ServiceWorker string   `json:"service_worker"` // MV3
	Type          string   `json:"type"`           // MV3: "module"
}

type ActionDef struct {
	DefaultPopup string          `json:"default_popup"`
	DefaultIcon  json.RawMessage `json:"default_icon"`
	DefaultTitle string          `json:"default_title"`
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

	retired   []any
	popupView *webkit.WebView
	natives   map[string]*nativeConn
	auths     []*authFlow
	helpers   map[string]*fetchHelper
	dnrStore  *webkit.UserContentFilterStore
	captures  map[string]*exec.Cmd
	b         *Browser
	secret    string
	reqSeq    int
	pending   map[int]func(json.RawMessage, string)
	ports     map[string]*extPort
	session   map[string]map[string]json.RawMessage // storage.session, memory only
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
