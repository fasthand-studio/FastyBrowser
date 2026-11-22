package main

// Extension management UI: the panel, the settings page section, and the
// toolbar button for browser actions.

import (
	"encoding/json"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	javascriptcore "github.com/diamondburned/gotk4-webkitgtk/pkg/javascriptcore/v6"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// showExtensionsPanel opens the full extensions list.
func showExtensionsPanel(b *Browser) {
	m := b.app.extensions
	p := newListPanel(b, "Extensions", "Search extensions")

	// Install button
	install := gtk.NewButtonFromIconName("list-add-symbolic")
	install.SetTooltipText("Install Extension from File")
	install.ConnectClicked(func() { installExtensionDialog(b) })
	p.extra.Append(install)

	// Store buttons
	chrome := gtk.NewButtonWithLabel("Chrome Store")
	chrome.AddCSSClass("flat")
	chrome.ConnectClicked(func() {
		b.openTab("https://chromewebstore.google.com/", true, nil, false, "", false)
		p.Close()
	})
	p.extra.Append(chrome)

	firefox := gtk.NewButtonWithLabel("Firefox Add-ons")
	firefox.AddCSSClass("flat")
	firefox.ConnectClicked(func() {
		b.openTab("https://addons.mozilla.org/", true, nil, false, "", false)
		p.Close()
	})
	p.extra.Append(firefox)

	p.empty.SetIconName("application-x-addon-symbolic")
	p.empty.SetTitle("No extensions installed")
	p.empty.SetDescription("Install .crx (Chrome) or .xpi (Firefox) extensions, or browse the stores.")

	p.populate = func(q string) int {
		n := 0
		for _, ext := range m.extensions {
			name := ext.Manifest.Name
			desc := ext.Manifest.Description
			version := ext.Manifest.Version
			if q != "" && !strings.Contains(strings.ToLower(name+desc), strings.ToLower(q)) {
				continue
			}
			sub := desc
			if sub == "" {
				sub = "v" + version
			} else if version != "" {
				sub = sub + " · v" + version
			}
			right := ext.Source
			if !ext.Enabled {
				right += " (disabled)"
			}

			eid := ext.ID
			icon := extIcon(ext)
			var btns []rowButton
			btns = append(btns, rowButton{
				icon: "emblem-system-symbolic",
				tip:  "Toggle Enabled",
				fn: func() {
					m.Toggle(eid)
					p.fill()
					b.toast("Restart to apply extension changes", 2)
				},
			})
			btns = append(btns, rowButton{
				icon: "user-trash-symbolic",
				tip:  "Remove Extension",
				fn: func() {
					confirmRemoveExtension(b, eid, func() { p.fill() })
				},
			})
			p.row(name, sub, right, ext, "", icon, btns...)
			n++
		}
		return n
	}
	p.activate = func(d any) {
		ext := d.(*Extension)
		if m.OpenPage(ext, "options") != nil {
			p.Close()
		}
	}
	p.show()
}

func extIcon(ext *Extension) gtk.Widgetter {
	// Try to load the extension's icon
	icons := ext.Manifest.Icons
	for _, size := range []string{"48", "32", "16", "128", "64", "24"} {
		if path, ok := icons[size]; ok {
			fullPath := filepath.Join(ext.Dir, path)
			if _, err := os.Stat(fullPath); err == nil {
				tex, err := gdk.NewTextureFromFilename(fullPath)
				if err == nil {
					img := gtk.NewImageFromPaintable(tex)
					img.SetPixelSize(24)
					return img
				}
			}
		}
	}
	img := gtk.NewImageFromIconName("application-x-addon-symbolic")
	img.SetPixelSize(24)
	return img
}

func installExtensionDialog(b *Browser) {
	fd := gtk.NewFileDialog()
	fd.SetTitle("Install Extension")
	f := gtk.NewFileFilter()
	f.SetName("Browser Extensions")
	f.AddPattern("*.crx")
	f.AddPattern("*.xpi")
	f.AddPattern("*.zip")
	filters := gio.NewListStore(gtk.GTypeFileFilter)
	filters.Append(f.Object)
	fd.SetFilters(filters)
	fd.Open(context.Background(), &b.Window, func(res gio.AsyncResulter) {
		var gf gio.Filer
		var err error
		try(func() { gf, err = fd.OpenFinish(res) })
		if err != nil || gf == nil {
			return
		}
		path := gf.Path()
		b.app.extensions.InstallDownloaded(path, func(name string, err error) {
			if err != nil {
				b.toast("Failed to install extension: "+err.Error(), 3)
			} else {
				b.toast("Installed \""+name+"\". Restart to activate.", 3)
				b.rebuildExtensionButtons()
			}
		})
	})
}

func confirmRemoveExtension(b *Browser, id string, done func()) {
	ext := b.app.extensions.find(id)
	if ext == nil {
		return
	}
	d := adw.NewAlertDialog("Remove "+ext.Manifest.Name+"?",
		"The extension and its data will be deleted.")
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("remove", "Remove")
	d.SetResponseAppearance("remove", adw.ResponseDestructive)
	d.ConnectResponse(func(r string) {
		if r == "remove" {
			b.app.extensions.Remove(id)
			b.toast("Removed "+ext.Manifest.Name, 2)
			b.rebuildExtensionButtons()
			done()
		}
	})
	d.Present(b)
}

// --- toolbar extension buttons -----------------------------------------------

// rebuildExtensionButtons adds browser action buttons to the sidebar footer
// (or top bar) for each enabled extension that has an action/browser_action.
func (b *Browser) rebuildExtensionButtons() {
	// Remove old extension buttons
	for _, btn := range b.extButtons {
		if hasParent(btn) {
			if p := btn.Parent(); p != nil {
				if box, ok := p.(*gtk.Box); ok {
					box.Remove(btn)
				}
			}
		}
	}
	b.extButtons = nil

	m := b.app.extensions
	if m == nil {
		return
	}
	for _, ext := range m.enabledList() {
		act := ext.action()
		if act == nil {
			continue
		}
		btn := extensionButton(b, ext)
		b.extButtons = append(b.extButtons, btn)
		b.sideFoot.Append(btn)
	}
}

func extensionButton(b *Browser, ext *Extension) *gtk.Button {
	title := ext.Manifest.Name
	if ext.title != "" {
		title = ext.title
	} else if act := ext.action(); act != nil && act.DefaultTitle != "" {
		title = act.DefaultTitle
	}

	var face gtk.Widgetter
	if icon := extIconSmall(ext); icon != nil {
		face = icon
	} else {
		face = gtk.NewLabel(string([]rune(strings.ToUpper(ext.Manifest.Name))[:1]))
	}
	var btn *gtk.Button
	if ext.badge != "" {
		ov := gtk.NewOverlay()
		ov.SetChild(face)
		badge := gtk.NewLabel(ext.badge)
		badge.AddCSSClass("ext-badge")
		badge.SetHAlign(gtk.AlignEnd)
		badge.SetVAlign(gtk.AlignEnd)
		if ext.badgeColor != "" {
			p := gtk.NewCSSProvider()
			p.LoadFromString(".ext-badge { background-color: " + ext.badgeColor + "; }")
			badge.StyleContext().AddProvider(p, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
		}
		ov.AddOverlay(badge)
		btn = gtk.NewButton()
		btn.SetChild(ov)
		title += " (" + ext.badge + ")"
	} else {
		btn = gtk.NewButton()
		btn.SetChild(face)
	}
	btn.SetTooltipText(title)
	btn.AddCSSClass("flat")
	btn.AddCSSClass("ext-button")

	eid := ext.ID
	btn.ConnectClicked(func() { openExtensionPopup(b, eid) })
	return btn
}

func extIconSmall(ext *Extension) gtk.Widgetter {
	paths := []string{}
	if ext.iconOverride != "" {
		paths = append(paths, ext.iconOverride)
	}
	icons := ext.Manifest.Icons
	for _, size := range []string{"16", "24", "32", "48"} {
		if p, ok := icons[size]; ok {
			paths = append(paths, p)
		}
	}
	for _, path := range paths {
		fullPath := filepath.Join(ext.Dir, strings.TrimLeft(path, "/"))
		if _, err := os.Stat(fullPath); err == nil {
			if tex, err := gdk.NewTextureFromFilename(fullPath); err == nil {
				img := gtk.NewImageFromPaintable(tex)
				img.SetPixelSize(16)
				return img
			}
		}
	}
	return nil
}

func openExtensionPopup(b *Browser, extID string) {
	m := b.app.extensions
	ext := m.find(extID)
	if ext == nil {
		return
	}
	page := ext.popupPath()
	if page == "" {
		m.PressAction(ext)
		return
	}

	d := adw.NewDialog()
	d.SetTitle(ext.Manifest.Name)
	d.SetContentWidth(360)
	d.SetContentHeight(480)

	tv := adw.NewToolbarView()
	tv.AddTopBar(adw.NewHeaderBar())

	ucm := b.app.engine.contentManager()
	ucm.AddScript(webkit.NewUserScript(m.preamble(ext, "page"),
		webkit.UserContentInjectAllFrames, webkit.UserScriptInjectAtDocumentStart, nil, nil))
	view := b.app.engine.newView(b.app.engine.session, ucm)
	ucm.ConnectScriptMessageReceived(func(v *javascriptcore.Value) {
		m.HandleMessage(v.ToJson(0), extSource{view: view})
	})
	view.SetVExpand(true)
	view.SetHExpand(true)
	view.ConnectClose(func() { d.Close() })
	m.popupView = view
	d.ConnectClosed(func() {
		if m.popupView == view {
			m.popupView = nil
		}
	})

	// A popup is as big as its content (25x25 up to 800x600), so measure the
	// page once it has loaded and again as it settles.
	fit := func() {
		m.evalResult(view, "", `if (!document.body) return [0, 0];
const cs = getComputedStyle(document.body);
let r = 0, b = 0;
const els = document.body.querySelectorAll('*');
for (let i = 0; i < els.length && i < 800; i++) {
  const q = els[i].getBoundingClientRect();
  if (q.width || q.height) { r = Math.max(r, q.right); b = Math.max(b, q.bottom); }
}
return [Math.ceil(Math.max(r, document.body.scrollWidth > innerWidth ? document.body.scrollWidth : 0) + (parseFloat(cs.marginRight) || 0)),
        Math.ceil(b + (parseFloat(cs.marginBottom) || 0))];`, true,
			func(out json.RawMessage, e string) {
				var v []float64
				if e != "" || json.Unmarshal(out, &v) != nil || len(v) < 2 {
					return
				}
				if v[0] == 0 && v[1] == 0 {
					return
				}
				d.SetContentWidth(min(max(int(v[0]), 120), 800))
				d.SetContentHeight(min(max(int(v[1])+48, 60), 650)) // +48: the title bar
			})
	}
	view.ConnectLoadChanged(func(ev webkit.LoadEvent) {
		if ev == webkit.LoadFinished {
			for _, ms := range []uint{150, 600, 1500} {
				glib.TimeoutAdd(ms, func() bool { fit(); return false })
			}
		}
	})
	view.LoadURI(extURL(ext.ID, page))

	tv.SetContent(view)
	d.SetChild(tv)
	d.Present(b)
}

// --- extension settings in the preferences dialog ----------------------------

func extensionSettingsGroup(b *Browser) *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle("Extensions")
	g.SetDescription("Chrome (.crx) and Firefox (.xpi) extensions")

	m := b.app.extensions
	if len(m.extensions) == 0 {
		r := adw.NewActionRow()
		r.SetTitle("No extensions installed")
		r.SetSubtitle("Use the Extensions panel to install from files or browse stores")
		g.Add(r)
	} else {
		for _, ext := range m.extensions {
			eid := ext.ID
			r := adw.NewSwitchRow()
			r.SetTitle(ext.Manifest.Name)
			sub := ext.Source + " · v" + ext.Manifest.Version
			if ext.Manifest.Description != "" {
				sub = ext.Manifest.Description
			}
			r.SetSubtitle(sub)
			r.SetActive(ext.Enabled)
			r.NotifyProperty("active", func() {
				m.Toggle(eid)
				b.toast("Restart to apply", 2)
			})
			g.Add(r)
		}
	}

	manage := adw.NewActionRow()
	manage.SetTitle("Manage extensions")
	suffixButton(manage, "Open", "", func() { showExtensionsPanel(b) })
	g.Add(manage)

	browse := adw.NewActionRow()
	browse.SetTitle("Browse extension stores")
	r1 := gtk.NewButtonWithLabel("Chrome")
	r1.AddCSSClass("flat")
	r1.SetVAlign(gtk.AlignCenter)
	r1.ConnectClicked(func() {
		b.openTab("https://chromewebstore.google.com/", true, nil, false, "", false)
	})
	manage.AddSuffix(r1)
	r2 := gtk.NewButtonWithLabel("Firefox")
	r2.AddCSSClass("flat")
	r2.SetVAlign(gtk.AlignCenter)
	r2.ConnectClicked(func() {
		b.openTab("https://addons.mozilla.org/", true, nil, false, "", false)
	})
	browse.AddSuffix(r1)
	browse.AddSuffix(r2)
	g.Add(browse)

	return g
}

// --- drag and drop .crx/.xpi onto the window ---------------------------------

func (b *Browser) setupExtensionDrop() {
	drop := gtk.NewDropTarget(coreglib.TypeString, gdk.ActionCopy)
	drop.ConnectDrop(func(v *coreglib.Value, x, y float64) bool {
		s, ok := v.GoValue().(string)
		if !ok {
			return false
		}
		s = strings.TrimSpace(s)
		lower := strings.ToLower(s)
		if strings.HasSuffix(lower, ".crx") || strings.HasSuffix(lower, ".xpi") || strings.HasSuffix(lower, ".zip") {
			path := s
			if strings.HasPrefix(path, "file://") {
				path = strings.TrimPrefix(path, "file://")
			}
			b.app.extensions.InstallDownloaded(path, func(name string, err error) {
				if err != nil {
					b.toast("Failed: "+err.Error(), 3)
				} else {
					b.toast("Installed \""+name+"\". Restart to activate.", 3)
					b.rebuildExtensionButtons()
				}
			})
			return true
		}
		return false
	})
	b.AddController(drop)
}

// extensionsBadge returns a short label for the sidebar showing extension count.
func extensionsBadge(m *ExtensionManager) string {
	n := len(m.enabledList())
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

// --- ensure the label type is imported for the unused reference guard ---------
var _ = pango.EllipsizeEnd
