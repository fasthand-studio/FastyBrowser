// Extension keyboard shortcuts.
package main

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
)

// --- keyboard commands -------------------------------------------------------------

type extShortcut struct {
	mods gdk.ModifierType
	key  uint
}

func parseShortcut(spec string) (extShortcut, bool) {
	var sc extShortcut
	parts := strings.Split(spec, "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i < len(parts)-1 {
			switch strings.ToLower(p) {
			case "ctrl", "command", "macctrl":
				sc.mods |= gdk.ControlMask
			case "alt", "option":
				sc.mods |= gdk.AltMask
			case "shift":
				sc.mods |= gdk.ShiftMask
			default:
				return sc, false
			}
			continue
		}
		names := map[string]string{"comma": "comma", "period": "period", "space": "space", "home": "Home", "end": "End",
			"pageup": "Page_Up", "pagedown": "Page_Down", "insert": "Insert", "delete": "Delete",
			"up": "Up", "down": "Down", "left": "Left", "right": "Right"}
		name := p
		if n, ok := names[strings.ToLower(p)]; ok {
			name = n
		}
		kv := gdk.KeyvalFromName(name)
		if kv == 0 {
			kv = gdk.KeyvalFromName(strings.ToLower(name))
		}
		if kv == 0 {
			return sc, false
		}
		sc.key = gdk.KeyvalToLower(kv)
	}
	return sc, sc.key != 0 && sc.mods != 0
}

func (e *Extension) shortcutFor(spec map[string]string) string {
	for _, k := range []string{"linux", "default"} {
		if v := spec[k]; v != "" {
			return v
		}
	}
	return ""
}

// runCommand fires the extension command bound to a key press, if any.
func (m *ExtensionManager) runCommand(kv uint, mods gdk.ModifierType) bool {
	for _, ext := range m.enabledList() {
		for name, c := range ext.Manifest.Commands {
			spec := ext.shortcutFor(c.SuggestedKey)
			if spec == "" {
				continue
			}
			sc, ok := parseShortcut(spec)
			if !ok || sc.key != kv || sc.mods != mods {
				continue
			}
			switch name {
			case "_execute_action", "_execute_browser_action", "_execute_page_action":
				m.PressAction(ext)
			default:
				var tab any
				if m.b != nil && m.b.current != nil {
					tab = m.tabInfo(m.b.current)
				}
				m.fireTo(ext, "commands.onCommand", name, tab)
			}
			return true
		}
	}
	return false
}
