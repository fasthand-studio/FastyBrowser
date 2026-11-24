package main

import (
	"context"
	"time"

	"github.com/diamondburned/gotk4-webkitgtk/pkg/soup/v3"
	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

// Instagram's "Log in with Facebook" asks facebook.com, from instagram.com,
// who is signed in (ig_xsite_user_info). That request carries Facebook's
// cookies only if they are SameSite=None. Here WebKitGTK keeps all of them
// as Lax, so Facebook answers "nobody", Instagram treats the sign-in as a
// new account sign-up, and its server then rejects the token
// (fb_profile: 400 invalid_response) and drops you on the login page.
// Facebook's cookies are meant for cross-site use, so after they change,
// mark Meta's secure cookies SameSite=None again. Only Meta's own domains
// are touched; other sites keep SameSite as they set it.
var metaSites = []string{
	"https://www.facebook.com/", "https://www.instagram.com/",
	"https://www.messenger.com/", "https://www.threads.net/",
}

func keepMetaCookiesCrossSite(cm *webkit.CookieManager) {
	var pending, fixing, again bool
	var schedule func()
	fix := func() {
		pending = false
		if fixing {
			return
		}
		fixing = true
		left := len(metaSites)
		done := func() {
			left--
			if left == 0 {
				// our own writes fire "changed"; let them settle first
				coreglib.TimeoutAdd(1500, func() bool {
					fixing = false
					if again {
						again = false
						schedule()
					}
					return false
				})
			}
		}
		for _, site := range metaSites {
			cm.Cookies(context.Background(), site, func(res gio.AsyncResulter) {
				defer done()
				var cs []*soup.Cookie
				var err error
				try(func() { cs, err = cm.CookiesFinish(res) })
				if err != nil {
					return
				}
				for _, c := range cs {
					if c.Secure() && c.SameSitePolicy() != soup.SameSitePolicyNone {
						c.SetSameSitePolicy(soup.SameSitePolicyNone)
						cm.AddCookie(context.Background(), c, func(r gio.AsyncResulter) { try(func() { cm.AddCookieFinish(r) }) })
					}
				}
			})
		}
	}
	schedule = func() {
		if fixing {
			again = true
			return
		}
		if !pending {
			pending = true
			coreglib.TimeoutAdd(uint(400*time.Millisecond/time.Millisecond), func() bool { fix(); return false })
		}
	}
	cm.ConnectChanged(schedule)
	coreglib.TimeoutAdd(2000, func() bool { fix(); return false })
}
