package main

// Where everything is kept: small JSON files in the user's data folder
// (Store.swift). Each file is written atomically, a moment after the last
// change, so a burst of changes is one write. The files are the same ones the
// first Linux port wrote, so nothing is lost moving to this one.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

const (
	appID   = "app.fastybrowser.Linux"
	appName = "fasty-browser"
	version = "1.0.1"
)

func xdgDir(env, fallback string) string {
	if d := os.Getenv(env); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

// privateDir makes the folder (if needed) readable by this user only. History,
// the open tabs, bookmarks and settings say a lot about a person, and these
// folders used to be open to every account on the computer. Setting the folder
// itself to 0700 also shuts out the files in it.
func privateDir(p string) string {
	os.MkdirAll(p, 0o700)
	if fi, err := os.Stat(p); err == nil && fi.Mode().Perm() != 0o700 {
		os.Chmod(p, 0o700)
	}
	return p
}

func dataDir() string {
	return privateDir(filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), appName))
}

func cacheDir() string {
	return privateDir(filepath.Join(xdgDir("XDG_CACHE_HOME", ".cache"), appName))
}

// hardenFiles makes the files directly in the data folder private too (older
// versions wrote them readable by everyone).
func hardenFiles() {
	dir := dataDir()
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				os.Chmod(filepath.Join(dir, e.Name()), 0o600)
			}
		}
	}
}

func dataPath(name string) string { return filepath.Join(dataDir(), name) }

func readJSON(name string, into any) bool {
	b, err := os.ReadFile(dataPath(name))
	if err != nil {
		return false
	}
	return json.Unmarshal(b, into) == nil
}

func writeJSON(name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	target := dataPath(name)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// saver writes a value a moment after it last changed.
type saver struct {
	name    string
	delay   uint
	pending coreglib.SourceHandle
	value   func() any
}

func (s *saver) changed() {
	if s.pending != 0 {
		return
	}
	s.pending = coreglib.TimeoutAdd(s.delay, func() bool {
		s.pending = 0
		s.write()
		return false
	})
}

func (s *saver) write() {
	if err := writeJSON(s.name, s.value()); err != nil {
		log.Printf("fasty: couldn't save %s: %v", s.name, err)
	}
}

func (s *saver) flush() {
	if s.pending != 0 {
		coreglib.SourceRemove(s.pending)
		s.pending = 0
		s.write()
	}
}

type WindowSize struct {
	W   int  `json:"w"`
	H   int  `json:"h"`
	Max bool `json:"max"`
}

type SettingsData struct {
	Engine               string             `json:"engine"`
	CustomEngine         string             `json:"custom_engine"`
	Appearance           string             `json:"appearance"` // system | light | dark
	TabsSide             string             `json:"tabs_side"`  // left | top
	SidebarFolded        bool               `json:"sidebar_folded"`
	SidebarWidth         int                `json:"sidebar_width"`
	Shield               bool               `json:"shield"`
	ShieldPaused         []string           `json:"shield_paused"`
	CleanLinks           bool               `json:"clean_links"`
	RestoreSession       bool               `json:"restore_session"`
	Zoom                 float64            `json:"zoom"`
	SiteZoom             map[string]float64 `json:"site_zoom"`
	TrackingPrevention   bool               `json:"tracking_prevention"`
	Spellcheck           bool               `json:"spellcheck"`
	DownloadsDir         string             `json:"downloads_dir"`
	SmoothScrolling      bool               `json:"smooth_scrolling"`
	HardwareAcceleration bool               `json:"hardware_acceleration"`
	MemorySaver          int                `json:"memory_saver"` // 1 once the sleep time has been moved from 30 to 5 minutes
	SleepAfterMinutes    int                `json:"sleep_after_minutes"`
	AuthenticatorButton  bool               `json:"authenticator_pinned"` // the Authenticator icon in the toolbar
	GPUVideoDecode       bool               `json:"gpu_video_decode"`     // decode video on an NVIDIA GPU (gpudecode.go)
	AgentSocket          bool               `json:"agent_socket"`         // the local control socket for scripts and agents (agent.go); off by default
	Permissions          map[string]bool    `json:"permissions"`
	Window               WindowSize         `json:"window"`
}

type Settings struct {
	SettingsData
	saver
	listeners []func(key string)
}

func loadSettings() *Settings {
	s := &Settings{SettingsData: SettingsData{
		Engine: "duckduckgo", Appearance: "system", TabsSide: "left", SidebarWidth: 248,
		Shield: true, ShieldPaused: []string{}, CleanLinks: true, RestoreSession: true, Zoom: 1.0,
		SiteZoom: map[string]float64{}, TrackingPrevention: true, Spellcheck: true,
		SmoothScrolling: true, HardwareAcceleration: true, SleepAfterMinutes: 5, MemorySaver: 1,
		Permissions: map[string]bool{}, Window: WindowSize{W: 1280, H: 820},
	}}
	readJSON("settings.json", &s.SettingsData)
	if s.MemorySaver < 1 {
		// Older versions slept tabs after 30 minutes, which kept a dozen pages
		// in memory; the default is 5 now. A time chosen on purpose is kept.
		if s.SleepAfterMinutes == 30 {
			s.SleepAfterMinutes = 5
		}
		s.MemorySaver = 1
	}
	if s.SiteZoom == nil {
		s.SiteZoom = map[string]float64{}
	}
	if s.Permissions == nil {
		s.Permissions = map[string]bool{}
	}
	if s.Zoom <= 0 {
		s.Zoom = 1
	}
	s.saver = saver{name: "settings.json", delay: 400, value: func() any { return s.SettingsData }}
	return s
}

// Set runs fn on the settings, saves them, and tells whoever listens which
// key changed.
func (s *Settings) Set(key string, fn func(*SettingsData)) {
	fn(&s.SettingsData)
	s.changed()
	for _, l := range s.listeners {
		l(key)
	}
}

func (s *Settings) Listen(fn func(key string)) { s.listeners = append(s.listeners, fn) }

// try runs fn and swallows a panic. The bindings panic when a C function that
// is documented never to return NULL does anyway (no favicon yet, no
// certificate, a cancelled file dialog); here that just means "nothing".
func try(fn func()) {
	defer func() { recover() }()
	fn()
}
