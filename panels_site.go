// The site card and the permission questions.
package main

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// siteCard is "about this site": is the connection private, is the blocker
// on here, what is hidden here.
func siteCard(b *Browser, anchor gtk.Widgetter) {
	t := b.current
	pop := gtk.NewPopover()
	pop.SetParent(anchor)
	box := gtk.NewBox(gtk.OrientationVertical, 10)
	box.AddCSSClass("site-card")
	if t == nil || t.url == "" {
		box.Append(label("No page open", "", pango.EllipsizeNone))
	} else {
		host := hostOf(t.url)
		if host == "" {
			host = t.url
		}
		box.Append(label(host, "title-4", pango.EllipsizeEnd))
		var secure string
		switch {
		case t.private:
			secure = "Private tab: nothing is kept after it closes."
		case strings.HasPrefix(t.url, "https://"):
			secure = "Connection is encrypted."
		case strings.HasPrefix(t.url, "http://"):
			secure = "Connection is not encrypted. Don't type passwords here."
		default:
			secure = scheme(t.url) + " page"
		}
		box.Append(label(secure, "dim-label", pango.EllipsizeNone))
		if t.view != nil && strings.HasPrefix(t.url, "https://") {
			var cert gio.TLSCertificater
			var ok bool
			try(func() { cert, _, ok = t.view.TLSInfo() })
			if ok && cert != nil {
				c := gio.BaseTLSCertificate(cert)
				issuer := c.IssuerName()
				txt := issuer
				if i := strings.LastIndex(issuer, "O="); i >= 0 {
					txt = strings.SplitN(issuer[i+2:], ",", 2)[0]
				}
				txt = "Certificate from " + txt
				if exp := c.NotValidAfter(); exp != nil {
					txt += "\nValid until " + exp.Format("%d %b %Y")
				}
				box.Append(label(txt, "dim-label caption", pango.EllipsizeNone))
			}
		}
		if b.app.settings.Shield && host != "" {
			row := gtk.NewBox(gtk.OrientationHorizontal, 12)
			row.Append(label("Block ads and trackers", "", pango.EllipsizeNone))
			sw := gtk.NewSwitch()
			sw.SetActive(!b.app.shield.Paused(host))
			sw.SetHExpand(true)
			sw.SetHAlign(gtk.AlignEnd)
			sw.NotifyProperty("active", func() {
				b.app.shield.Pause(host, !sw.Active())
				b.retuneShield()
				b.reload(t, false)
			})
			row.Append(sw)
			box.Append(row)
		}
		veils := b.app.curtain.Veils(host)
		text := "Hide something on this page…"
		if len(veils) > 0 {
			text = fmt.Sprintf("%d hidden on this site…", len(veils))
		}
		hid := gtk.NewButtonWithLabel(text)
		hid.AddCSSClass("flat")
		hid.ConnectClicked(func() {
			pop.Popdown()
			if len(veils) > 0 {
				b.showHidden()
			} else {
				b.startPicking()
			}
		})
		box.Append(hid)
		if t.view != nil {
			if z := t.view.ZoomLevel(); z < 0.99 || z > 1.01 {
				zb := gtk.NewButtonWithLabel(fmt.Sprintf("Zoom %d%% — reset", int(z*100+0.5)))
				zb.AddCSSClass("flat")
				zb.ConnectClicked(func() { b.zoom(0); pop.Popdown() })
				box.Append(zb)
			}
		}
		if !t.private {
			clear := gtk.NewButtonWithLabel("Clear cookies and data for this site")
			clear.AddCSSClass("flat")
			clear.AddCSSClass("error")
			clear.ConnectClicked(func() { b.clearSiteData(host, false); pop.Popdown() })
			box.Append(clear)
		}
	}
	pop.SetChild(box)
	pop.ConnectClosed(func() { coreglib.IdleAdd(func() { pop.Unparent() }) })
	pop.Popup()
}

func permissionKind(req webkit.PermissionRequester) (string, string) {
	switch r := req.(type) {
	case *webkit.UserMediaPermissionRequest:
		cam := webkit.UserMediaPermissionIsForVideoDevice(r)
		mic := webkit.UserMediaPermissionIsForAudioDevice(r)
		switch {
		case webkit.UserMediaPermissionIsForDisplayDevice(r):
			return "UserMediaPermissionRequest", "see your screen"
		case cam && mic:
			return "UserMediaPermissionRequest", "use your camera and microphone"
		case cam:
			return "UserMediaPermissionRequest", "use your camera"
		}
		return "UserMediaPermissionRequest", "use your microphone"
	case *webkit.GeolocationPermissionRequest:
		return "GeolocationPermissionRequest", "know your location"
	case *webkit.NotificationPermissionRequest:
		return "NotificationPermissionRequest", "show notifications"
	case *webkit.DeviceInfoPermissionRequest:
		return "DeviceInfoPermissionRequest", "see which cameras and microphones you have"
	case *webkit.MediaKeySystemPermissionRequest:
		return "MediaKeySystemPermissionRequest", "play protected content"
	case *webkit.WebsiteDataAccessPermissionRequest:
		return "WebsiteDataAccessPermissionRequest", "use its cookies on this site"
	}
	return "PermissionRequest", "do something that needs your permission"
}

func askPermission(b *Browser, t *Tab, req webkit.PermissionRequester) {
	host := hostOf(t.url)
	if host == "" {
		host = "This page"
	}
	kind, what := permissionKind(req)
	key := host + "|" + kind
	if allow, ok := b.app.settings.Permissions[key]; ok {
		if allow {
			req.Allow()
		} else {
			req.Deny()
		}
		return
	}
	d := adw.NewAlertDialog(host+" wants to "+what, "")
	d.AddResponse("deny", "Don't Allow")
	d.AddResponse("allow", "Allow")
	d.SetResponseAppearance("allow", adw.ResponseSuggested)
	d.SetDefaultResponse("deny")
	d.SetCloseResponse("deny")
	remember := gtk.NewCheckButtonWithLabel("Remember for this site")
	d.SetExtraChild(remember)
	d.ConnectResponse(func(r string) {
		allow := r == "allow"
		if remember.Active() {
			b.app.settings.Set("permissions", func(s *SettingsData) { s.Permissions[key] = allow })
		}
		if allow {
			req.Allow()
		} else {
			req.Deny()
		}
	})
	d.Present(b)
}
