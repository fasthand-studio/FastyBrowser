// The Settings window.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func switchRow(b *Browser, title, key string, get func(*SettingsData) *bool, restart bool) *adw.SwitchRow {
	r := adw.NewSwitchRow()
	r.SetTitle(title)
	r.SetActive(*get(&b.app.settings.SettingsData))
	if restart {
		r.SetSubtitle("Takes effect next launch")
	}
	r.NotifyProperty("active", func() {
		b.app.settings.Set(key, func(s *SettingsData) { *get(s) = r.Active() })
	})
	return r
}

func comboRow(title, subtitle string, names []string, selected int, onPick func(i int)) *adw.ComboRow {
	r := adw.NewComboRow()
	r.SetTitle(title)
	if subtitle != "" {
		r.SetSubtitle(subtitle)
	}
	r.SetModel(gtk.NewStringList(names))
	if selected >= 0 {
		r.SetSelected(uint(selected))
	}
	r.NotifyProperty("selected", func() { onPick(int(r.Selected())) })
	return r
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return 0
}

func suffixButton(row *adw.ActionRow, text string, classes string, fn func()) *gtk.Button {
	btn := gtk.NewButtonWithLabel(text)
	btn.SetVAlign(gtk.AlignCenter)
	for _, c := range strings.Fields(classes) {
		btn.AddCSSClass(c)
	}
	btn.ConnectClicked(fn)
	row.AddSuffix(btn)
	return btn
}

func showSettings(b *Browser) {
	s := b.app.settings
	d := adw.NewPreferencesDialog()
	d.SetTitle("Settings")
	d.SetSearchEnabled(true)
	releaseToasts := b.toastInto(d.AddToast) // toasts inside the window, not under it
	d.ConnectClosed(releaseToasts)

	// General
	page := adw.NewPreferencesPage()
	page.SetTitle("General")
	page.SetIconName("preferences-system-symbolic")
	g := adw.NewPreferencesGroup()
	g.SetTitle("Searching")
	var keys, names []string
	for _, e := range engines {
		keys = append(keys, e.key)
		names = append(names, e.name)
	}
	custom := adw.NewEntryRow()
	custom.SetTitle("Custom search address (%s for the words)")
	custom.SetText(s.CustomEngine)
	custom.SetVisible(s.Engine == "custom")
	custom.ConnectChanged(func() { s.Set("custom_engine", func(d *SettingsData) { d.CustomEngine = custom.Text() }) })
	g.Add(comboRow("Search engine", "", names, indexOf(keys, s.Engine), func(i int) {
		s.Set("engine", func(d *SettingsData) { d.Engine = keys[i] })
		custom.SetVisible(keys[i] == "custom")
	}))
	g.Add(custom)
	page.Add(g)

	g = adw.NewPreferencesGroup()
	g.SetTitle("Appearance")
	looks := []string{"system", "light", "dark"}
	g.Add(comboRow("Light, dark, or the system's own", "", []string{"System", "Light", "Dark"},
		indexOf(looks, s.Appearance), func(i int) { s.Set("appearance", func(d *SettingsData) { d.Appearance = looks[i] }) }))
	sides := []string{"left", "top"}
	g.Add(comboRow("Tabs", "Ctrl+Shift+S switches", []string{"Down the left", "Across the top"},
		indexOf(sides, s.TabsSide), func(i int) { s.Set("tabs_side", func(d *SettingsData) { d.TabsSide = sides[i] }) }))
	zoom := adw.NewSpinRowWithRange(30, 300, 10)
	zoom.SetTitle("Page zoom (%)")
	zoom.SetValue(float64(int(s.Zoom*100 + 0.5)))
	zoom.NotifyProperty("value", func() { b.setDefaultZoom(zoom.Value() / 100) })
	g.Add(zoom)
	page.Add(g)

	g = adw.NewPreferencesGroup()
	g.SetTitle("Starting up and speed")
	g.Add(switchRow(b, "Reopen last session's tabs", "restore_session", func(d *SettingsData) *bool { return &d.RestoreSession }, false))
	sleep := adw.NewSpinRowWithRange(0, 240, 5)
	sleep.SetTitle("Put unused tabs to sleep after (minutes)")
	sleep.SetSubtitle("Frees their memory; 0 never does")
	sleep.SetValue(float64(s.SleepAfterMinutes))
	sleep.NotifyProperty("value", func() {
		s.Set("sleep_after_minutes", func(d *SettingsData) { d.SleepAfterMinutes = int(sleep.Value()) })
	})
	g.Add(sleep)
	g.Add(switchRow(b, "Check spelling while typing", "spellcheck", func(d *SettingsData) *bool { return &d.Spellcheck }, true))
	g.Add(switchRow(b, "Smooth scrolling", "smooth_scrolling", func(d *SettingsData) *bool { return &d.SmoothScrolling }, true))
	g.Add(switchRow(b, "Hardware acceleration", "hardware_acceleration", func(d *SettingsData) *bool { return &d.HardwareAcceleration }, true))
	gpu := switchRow(b, "GPU video decoding (NVIDIA, experimental)", "gpu_video_decode", func(d *SettingsData) *bool { return &d.GPUVideoDecode }, true)
	ok, line := gpuDecodeStatus()
	gpu.SetSubtitle(line)
	gpu.SetSensitive(ok)
	g.Add(gpu)
	def := adw.NewActionRow()
	def.SetTitle("Default browser")
	def.SetSubtitle("Open links from other apps in Fasty")
	suffixButton(def, "Make Default", "", b.makeDefault)
	g.Add(def)
	page.Add(g)

	g = adw.NewPreferencesGroup()
	g.SetTitle("Downloads")
	dl := adw.NewActionRow()
	dl.SetTitle("Save files to")
	dl.SetSubtitle(b.app.downloads.folder())
	suffixButton(dl, "Choose…", "", func() {
		fd := gtk.NewFileDialog()
		fd.SetTitle("Save downloads to")
		fd.SelectFolder(context.Background(), &b.Window, func(res gio.AsyncResulter) {
			var f gio.Filer
			var err error
			try(func() { f, err = fd.SelectFolderFinish(res) })
			if err != nil || f == nil {
				return
			}
			p := f.Path()
			s.Set("downloads_dir", func(d *SettingsData) { d.DownloadsDir = p })
			dl.SetSubtitle(p)
		})
	})
	g.Add(dl)
	page.Add(g)
	d.Add(page)

	// Extensions
	page.Add(extensionSettingsGroup(b))

	// Privacy
	page = adw.NewPreferencesPage()
	page.SetTitle("Privacy")
	page.SetIconName("security-high-symbolic")
	g = adw.NewPreferencesGroup()
	g.SetTitle("Blocking")
	g.SetDescription("EasyList, EasyPrivacy and StevenBlack hosts, compiled with Brave's adblock-rust, stop ads and trackers at the network level, before the page asks for them.")
	g.Add(switchRow(b, "Block ads and trackers", "shield", func(d *SettingsData) *bool { return &d.Shield }, false))
	g.Add(switchRow(b, "Remove tracking parameters from links", "clean_links", func(d *SettingsData) *bool { return &d.CleanLinks }, false))
	g.Add(switchRow(b, "Prevent cross-site tracking", "tracking_prevention", func(d *SettingsData) *bool { return &d.TrackingPrevention }, true))
	page.Add(g)
	page.Add(authenticatorGroup(b))
	page.Add(passkeysGroup(b))
	if len(s.ShieldPaused) > 0 {
		g := adw.NewPreferencesGroup()
		g.SetTitle("Blocker is off on")
		for _, host := range s.ShieldPaused {
			host := host
			r := adw.NewActionRow()
			r.SetTitle(host)
			btn := gtk.NewButtonFromIconName("user-trash-symbolic")
			btn.SetVAlign(gtk.AlignCenter)
			btn.AddCSSClass("flat")
			btn.ConnectClicked(func() {
				b.app.shield.Pause(host, false)
				b.retuneShield()
				g.Remove(r)
			})
			r.AddSuffix(btn)
			g.Add(r)
		}
		page.Add(g)
	}
	if hosts := b.app.curtain.Hosts(); len(hosts) > 0 {
		g := adw.NewPreferencesGroup()
		g.SetTitle("Hidden elements")
		for _, host := range hosts {
			host := host
			r := adw.NewActionRow()
			r.SetTitle(host)
			r.SetSubtitle(fmt.Sprintf("%d hidden", len(b.app.curtain.Veils(host))))
			suffixButton(r, "Show All", "flat", func() { b.app.curtain.RestoreAll(host); g.Remove(r) })
			g.Add(r)
		}
		page.Add(g)
	}
	g = adw.NewPreferencesGroup()
	g.SetTitle("Browsing data")
	r := adw.NewActionRow()
	r.SetTitle("Clear everything")
	r.SetSubtitle("History, cookies, site data and cache. Bookmarks stay.")
	suffixButton(r, "Clear…", "destructive-action", b.confirmClearAll)
	g.Add(r)
	r = adw.NewActionRow()
	r.SetTitle("Import bookmarks")
	r.SetSubtitle("From an HTML bookmarks file (Chrome, Firefox…)")
	suffixButton(r, "Import…", "", b.importBookmarks)
	g.Add(r)
	page.Add(g)
	d.Add(page)

	// Open source credits: each row names the project, its licence and a link
	// to the upstream licence or project page.
	page = adw.NewPreferencesPage()
	page.SetTitle("Open Source")
	page.SetIconName("help-about-symbolic")
	g = adw.NewPreferencesGroup()
	g.SetTitle("Open source licenses")
	g.SetDescription("Fasty is built on the work of these projects.")
	type credit struct {
		name, license, url string
	}
	credits := []credit{
		{"Fasty Browser", "MIT", "https://github.com/fasthand-studio/Fasty-Browser/blob/main/LICENSE"},
		{"Go", "BSD-3-Clause", "https://go.dev/LICENSE"},
		{"GTK 4", "LGPL-2.1-or-later", "https://www.gtk.org/"},
		{"libadwaita", "LGPL-2.1-or-later", "https://gitlab.gnome.org/GNOME/libadwaita"},
		{"WebKitGTK 6.0", "LGPL-2.1-or-later / BSD-3-Clause", "https://webkit.org/licensing/"},
		{"GStreamer", "LGPL-2.1-or-later", "https://gstreamer.freedesktop.org/"},
		{"gotk4 bindings", "MPL-2.0", "https://github.com/diamondburned/gotk4"},
		{"golang.org/x/crypto", "BSD-3-Clause", "https://cs.opensource.google/go/x/crypto/+/master:LICENSE"},
		{"adblock-rust", "MPL-2.0", "https://github.com/brave/adblock-rust/blob/master/LICENSE"},
		{"serde_json", "MIT OR Apache-2.0", "https://github.com/serde-rs/json"},
		{"EasyList / EasyPrivacy", "GPL-3.0-or-later OR CC BY-SA 3.0-or-later", "https://easylist.to/pages/licence.html"},
		{"StevenBlack hosts", "MIT", "https://github.com/StevenBlack/hosts/blob/master/license.txt"},
	}
	for _, c := range credits {
		c := c
		r := adw.NewActionRow()
		r.SetTitle(c.name)
		r.SetSubtitle(c.license)
		suffixButton(r, "View", "flat", func() {
			b.openTab(c.url, true, nil, false, "", false)
		})
		g.Add(r)
	}
	page.Add(g)
	d.Add(page)
	d.Present(b)
}

// passkeysGroup lists the passkeys saved on this computer, each with a way to
// delete it.
func passkeysGroup(b *Browser) *adw.PreferencesGroup {
	g := adw.NewPreferencesGroup()
	g.SetTitle("Passkeys")
	g.SetDescription("Passkeys sites have saved on this computer. They don't sync, and deleting one only removes it here: remove it from the site's own security settings too.")
	store := b.app.passkeys
	var rows []*adw.ActionRow
	var refresh func()
	refresh = func() {
		for _, r := range rows {
			g.Remove(r)
		}
		rows = nil
		store.mu.Lock()
		list := append([]passkey(nil), store.list...)
		store.mu.Unlock()
		if len(list) == 0 {
			r := adw.NewActionRow()
			r.SetTitle("No passkeys saved")
			g.Add(r)
			rows = append(rows, r)
			return
		}
		for _, k := range list {
			k := k
			r := adw.NewActionRow()
			r.SetTitle(k.RPID)
			who := k.UserName
			if who == "" {
				who = k.DisplayName
			}
			r.SetSubtitle(who + " · " + time.Unix(k.Created, 0).Format("2 Jan 2006"))
			suffixButton(r, "Delete", "", func() {
				d := adw.NewAlertDialog("Delete this passkey?", "You will not be able to sign in to "+k.RPID+" with it on this computer any more.")
				d.AddResponse("cancel", "Cancel")
				d.AddResponse("delete", "Delete")
				d.SetResponseAppearance("delete", adw.ResponseDestructive)
				d.SetCloseResponse("cancel")
				d.ConnectResponse(func(resp string) {
					if resp != "delete" {
						return
					}
					store.mu.Lock()
					for i, p := range store.list {
						if p.ID == k.ID {
							store.list = append(store.list[:i], store.list[i+1:]...)
							break
						}
					}
					store.save()
					store.mu.Unlock()
					refresh()
				})
				d.Present(b)
			})
			g.Add(r)
			rows = append(rows, r)
		}
	}
	refresh()
	return g
}
