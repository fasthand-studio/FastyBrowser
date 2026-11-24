package main

import (
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
)

// WebKit's favicon database forgets icons, so after a while tabs (pinned
// ones above all) come back showing only a letter. Fasty keeps its own copy of
// each site's icon, one small PNG per host, and uses it until the page gives
// a fresh one. Nothing else is cached here, and private tabs never use it.

var iconHost = regexp.MustCompile(`^[a-z0-9.\-]+$`)

func iconPath(host string) string {
	if !iconHost.MatchString(host) {
		return ""
	}
	return filepath.Join(dataDir(), "favicons", host+".png")
}

// saveIcon stores tex for host unless a recent copy is already there.
func saveIcon(host string, tex gdk.Texturer) {
	p := iconPath(host)
	if p == "" || tex == nil {
		return
	}
	if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		return
	}
	os.MkdirAll(filepath.Dir(p), 0o700)
	try(func() { gdk.BaseTexture(tex).SaveToPNG(p) })
	os.Chmod(p, 0o600)
}

// savedIcon is the stored icon for host, or nil.
func savedIcon(host string) gdk.Paintabler {
	p := iconPath(host)
	if p == "" {
		return nil
	}
	var tex gdk.Paintabler
	try(func() {
		if t, err := gdk.NewTextureFromFilename(p); err == nil {
			tex = t
		}
	})
	return tex
}

func clearSavedIcons() { os.RemoveAll(filepath.Join(dataDir(), "favicons")) }
