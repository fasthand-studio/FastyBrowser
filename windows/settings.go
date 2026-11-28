package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Settings are the user's choices, stored as JSON under %AppData%\FastyBrowser.
type Settings struct {
	Homepage       string `json:"homepage"`
	SearchEngine   string `json:"searchEngine"`
	Shield         bool   `json:"shield"`
	ExtendedShield bool   `json:"extendedShield"`
	SleepMinutes   int    `json:"sleepMinutes"`
	RestoreSession bool   `json:"restoreSession"`
	// Same choices as the Linux build.
	Appearance    string   `json:"appearance"` // system | light | dark
	TabsSide      string   `json:"tabsSide"`   // left | top
	SidebarFolded bool     `json:"sidebarFolded"`
	SidebarWidth  int      `json:"sidebarWidth"`
	CleanLinks    bool     `json:"cleanLinks"`
	CustomEngine  string   `json:"customEngine"`  // search address with %s, used when SearchEngine is "custom"
	PageZoom      int      `json:"pageZoom"`      // percent, 50 to 300
	SmoothScroll  bool     `json:"smoothScroll"`  // takes effect next launch
	HardwareAccel bool     `json:"hardwareAccel"` // takes effect next launch
	ShieldPaused  []string `json:"shieldPaused"`  // sites where Shield is off
}

func DefaultSettings() Settings {
	return Settings{
		Homepage:       "",
		SearchEngine:   "duckduckgo",
		Shield:         true,
		ExtendedShield: true,
		SleepMinutes:   5, // same default as the Linux build
		RestoreSession: true,
		Appearance:     "system",
		TabsSide:       "left",
		SidebarWidth:   248, // same as Linux
		CleanLinks:     true,
		PageZoom:       100,
		SmoothScroll:   true,
		HardwareAccel:  true,
		ShieldPaused:   []string{},
	}
}

// Normalize clamps values from disk or from the settings page to something safe.
func (s Settings) Normalize() Settings {
	if _, ok := searchEngines[s.SearchEngine]; !ok && s.SearchEngine != "custom" {
		s.SearchEngine = "duckduckgo"
	}
	if s.SearchEngine == "custom" && !strings.Contains(s.CustomEngine, "%s") {
		s.SearchEngine = "duckduckgo"
	}
	if s.PageZoom == 0 {
		s.PageZoom = 100
	}
	if s.PageZoom < 50 {
		s.PageZoom = 50
	}
	if s.PageZoom > 300 {
		s.PageZoom = 300
	}
	if s.ShieldPaused == nil {
		s.ShieldPaused = []string{}
	}
	switch s.Appearance {
	case "light", "dark", "system":
	default:
		s.Appearance = "system"
	}
	if s.TabsSide != "top" {
		s.TabsSide = "left"
	}
	if s.SidebarWidth < 180 {
		s.SidebarWidth = 180
	}
	if s.SidebarWidth > 420 {
		s.SidebarWidth = 420
	}
	if s.SleepMinutes < 0 {
		s.SleepMinutes = 0
	}
	if s.SleepMinutes > 1440 {
		s.SleepMinutes = 1440
	}
	if s.Homepage != "" {
		// A homepage must be an address; text that would turn into a search is dropped.
		if IsAddress(s.Homepage) {
			s.Homepage = ResolveInput(s.Homepage, s.SearchEngine)
		} else {
			s.Homepage = ""
		}
	}
	return s
}

func LoadSettings(dir string) Settings {
	s := DefaultSettings() // fields missing from an older file keep these defaults
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return s
	}
	if json.Unmarshal(b, &s) != nil {
		return DefaultSettings()
	}
	return s.Normalize()
}

func SaveSettings(dir string, s Settings) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.Normalize(), "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "settings.json"), b)
}

// writeFileAtomic writes next to the target and renames, so a crash never leaves half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Session is the list of page URLs open at exit.
type Session struct {
	URLs   []string `json:"urls"`
	Pinned []bool   `json:"pinned,omitempty"`
	Active int      `json:"active"`
}

func LoadSession(dir string) (Session, error) {
	var s Session
	b, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Session{}, err
	}
	if s.Active < 0 || s.Active >= len(s.URLs) {
		s.Active = 0
	}
	return s, nil
}

func SaveSession(dir string, s Session) error {
	if len(s.URLs) == 0 {
		return errors.New("empty session")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	return writeFileAtomic(filepath.Join(dir, "session.json"), b)
}
