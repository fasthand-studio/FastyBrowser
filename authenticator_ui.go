package main

// The Authenticator window (Settings > Privacy > Authenticator): each saved
// account with its current code, a ring counting down to the next one, and
// the code after it. Click a code to copy it.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/cairo"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// showAuthenticator opens the window, asking for the PIN first when one is set.
func showAuthenticator(b *Browser) {
	v := b.app.authenticatorVault()
	switch err := v.Open(); {
	case err == errLocked:
		askAuthPIN(b, "Enter your PIN to open the Authenticator.", func() { showAuthenticatorList(b) })
	case err != nil:
		b.toast("Couldn't open the Authenticator: "+err.Error(), 3)
	default:
		showAuthenticatorList(b)
	}
}

// askAuthPIN asks for the PIN until it is right or you cancel, then calls ok.
func askAuthPIN(b *Browser, message string, ok func()) {
	v := b.app.authenticatorVault()
	d := adw.NewAlertDialog("Authenticator PIN", message)
	d.AddResponse("cancel", "Cancel")
	d.AddResponse("unlock", "Unlock")
	d.SetResponseAppearance("unlock", adw.ResponseSuggested)
	d.SetDefaultResponse("unlock")
	d.SetCloseResponse("cancel")
	entry := gtk.NewPasswordEntry()
	entry.SetShowPeekIcon(true)
	entry.SetObjectProperty("activates-default", true)
	d.SetExtraChild(entry)
	if w := v.WaitFor(); w > 0 {
		d.SetBody(fmt.Sprintf("Too many wrong tries. Wait %d seconds.", w))
	}
	d.ConnectResponse(func(r string) {
		if r != "unlock" {
			return
		}
		if err := v.Unlock(entry.Text()); err != nil {
			askAuthPIN(b, capitalFirst(err.Error())+".", ok)
			return
		}
		ok()
	})
	d.Present(b)
	entry.GrabFocus()
}

func capitalFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type authRow struct {
	entry authEntry
	row   *gtk.ListBoxRow
	code  *gtk.Label
	next  *gtk.Label
	secs  *gtk.Label
	ring  *gtk.DrawingArea
}

func showAuthenticatorList(b *Browser) {
	v := b.app.authenticatorVault()
	d := adw.NewDialog()
	d.SetTitle("Authenticator")
	d.SetContentWidth(520)
	d.SetContentHeight(640)

	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	add := gtk.NewButtonFromIconName("list-add-symbolic")
	add.SetTooltipText("Add an account")
	hb.PackEnd(add)
	tv.AddTopBar(hb)

	box := gtk.NewBox(gtk.OrientationVertical, 10)
	box.SetMarginStart(14)
	box.SetMarginEnd(14)
	box.SetMarginBottom(14)
	search := gtk.NewSearchEntry()
	search.SetPlaceholderText("Search")
	box.Append(search)

	list := gtk.NewListBox()
	list.AddCSSClass("boxed-list-separate")
	list.SetSelectionMode(gtk.SelectionNone)
	list.SetActivateOnSingleClick(true)
	empty := adw.NewStatusPage()
	empty.SetIconName("dialog-password-symbolic")
	empty.SetTitle("No codes yet")
	empty.SetDescription("Add the secret key or otpauth:// link a site gives you when you turn on two-step sign-in.")
	emptyAdd := gtk.NewButtonWithLabel("Add an Account")
	emptyAdd.AddCSSClass("pill")
	emptyAdd.AddCSSClass("suggested-action")
	emptyAdd.SetHAlign(gtk.AlignCenter)
	empty.SetChild(emptyAdd)
	stack := gtk.NewStack()
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sw.SetVExpand(true)
	sw.SetChild(list)
	stack.AddNamed(sw, "list")
	stack.AddNamed(empty, "empty")
	stack.SetVExpand(true)
	box.Append(stack)
	tv.SetContent(box)
	d.SetChild(tv)

	var rows []*authRow
	var rebuild func()
	var editor func(e *authEntry)

	update := func() {
		now := time.Now()
		for _, r := range rows {
			e := r.entry.normalized()
			code, err := totpAt(e, now)
			if err != nil {
				code = "······"
			}
			nextCode, _ := totpAt(e, now.Add(time.Duration(e.Period)*time.Second))
			left := e.secondsLeft(now)
			r.code.SetText(groupCode(code))
			r.next.SetText(groupCode(nextCode))
			r.secs.SetText(fmt.Sprint(left))
			r.ring.QueueDraw()
			if left <= 5 {
				r.code.AddCSSClass("error")
			} else {
				r.code.RemoveCSSClass("error")
			}
		}
	}

	rebuild = func() {
		list.RemoveAll()
		rows = nil
		all := v.List()
		q := strings.ToLower(strings.TrimSpace(search.Text()))
		for _, e := range all {
			if q != "" && !strings.Contains(strings.ToLower(e.Issuer+" "+e.Account), q) {
				continue
			}
			r := buildAuthRow(b, e, v, rebuild, editor)
			rows = append(rows, r)
			list.Append(r.row)
		}
		if len(all) == 0 {
			stack.SetVisibleChildName("empty")
		} else {
			stack.SetVisibleChildName("list")
		}
		update()
	}

	editor = func(e *authEntry) { showAuthEntryEditor(b, e, rebuild) }
	add.ConnectClicked(func() { editor(nil) })
	emptyAdd.ConnectClicked(func() { editor(nil) })
	search.ConnectSearchChanged(rebuild)
	list.ConnectRowActivated(func(row *gtk.ListBoxRow) {
		for _, r := range rows {
			if r.row.Index() == row.Index() {
				code, err := totpAt(r.entry, time.Now())
				if err == nil {
					b.copyText(code)
				}
			}
		}
	})

	closed := false
	coreglib.TimeoutAdd(500, func() bool {
		if closed {
			return false
		}
		update()
		return true
	})
	d.ConnectClosed(func() {
		closed = true
		v.Lock() // the codes are gone from memory; a PIN is asked again next time
	})
	rebuild()
	d.Present(b)
}

func buildAuthRow(b *Browser, e authEntry, v *authVault, rebuild func(), editor func(*authEntry)) *authRow {
	r := &authRow{entry: e}
	r.row = gtk.NewListBoxRow()
	r.row.SetActivatable(true)

	card := gtk.NewBox(gtk.OrientationVertical, 8)
	card.SetMarginTop(12)
	card.SetMarginBottom(12)
	card.SetMarginStart(14)
	card.SetMarginEnd(14)

	top := gtk.NewBox(gtk.OrientationHorizontal, 10)
	names := gtk.NewBox(gtk.OrientationVertical, 2)
	names.SetHExpand(true)
	title := e.Issuer
	if title == "" {
		title = e.Account
	}
	t := gtk.NewLabel(title)
	t.SetXAlign(0)
	t.AddCSSClass("heading")
	t.SetEllipsize(3)
	names.Append(t)
	if e.Issuer != "" && e.Account != "" {
		a := gtk.NewLabel(e.Account)
		a.SetXAlign(0)
		a.AddCSSClass("dim-label")
		a.AddCSSClass("caption")
		a.SetEllipsize(3)
		names.Append(a)
	}
	top.Append(names)

	r.ring = gtk.NewDrawingArea()
	r.ring.SetContentWidth(38)
	r.ring.SetContentHeight(38)
	r.ring.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, w, h int) {
		now := time.Now()
		en := r.entry.normalized()
		frac := float64(en.Period-int(now.Unix()%int64(en.Period))) / float64(en.Period)
		cx, cy := float64(w)/2, float64(h)/2
		rad := math.Min(cx, cy) - 3
		cr.SetLineWidth(4)
		cr.SetSourceRGBA(0.43, 0.29, 1, 0.15)
		cr.Arc(cx, cy, rad, 0, 2*math.Pi)
		cr.Stroke()
		cr.SetSourceRGBA(0.43, 0.29, 1, 1)
		cr.Arc(cx, cy, rad, -math.Pi/2, -math.Pi/2+2*math.Pi*frac)
		cr.Stroke()
	})
	r.secs = gtk.NewLabel("")
	r.secs.AddCSSClass("caption-heading")
	ov := gtk.NewOverlay()
	ov.SetChild(r.ring)
	r.secs.SetHAlign(gtk.AlignCenter)
	r.secs.SetVAlign(gtk.AlignCenter)
	ov.AddOverlay(r.secs)
	top.Append(ov)

	more := gtk.NewMenuButton()
	more.SetIconName("view-more-symbolic")
	more.AddCSSClass("flat")
	more.SetVAlign(gtk.AlignCenter)
	pop := gtk.NewPopover()
	pb := gtk.NewBox(gtk.OrientationVertical, 2)
	edit := gtk.NewButtonWithLabel("Edit…")
	edit.AddCSSClass("flat")
	del := gtk.NewButtonWithLabel("Delete…")
	del.AddCSSClass("flat")
	del.AddCSSClass("destructive-action")
	pb.Append(edit)
	pb.Append(del)
	pop.SetChild(pb)
	more.SetPopover(pop)
	top.Append(more)
	card.Append(top)

	card.Append(gtk.NewSeparator(gtk.OrientationHorizontal))

	bottom := gtk.NewBox(gtk.OrientationHorizontal, 10)
	r.code = gtk.NewLabel("")
	r.code.SetXAlign(0)
	r.code.SetHExpand(true)
	r.code.AddCSSClass("title-1")
	r.code.AddCSSClass("numeric")
	r.code.SetSelectable(false)
	bottom.Append(r.code)
	nx := gtk.NewBox(gtk.OrientationVertical, 0)
	nx.SetVAlign(gtk.AlignCenter)
	nl := gtk.NewLabel("Next")
	nl.SetXAlign(1)
	nl.AddCSSClass("dim-label")
	nl.AddCSSClass("caption")
	r.next = gtk.NewLabel("")
	r.next.SetXAlign(1)
	r.next.AddCSSClass("numeric")
	r.next.AddCSSClass("dim-label")
	nx.Append(nl)
	nx.Append(r.next)
	bottom.Append(nx)
	card.Append(bottom)
	r.row.SetChild(card)

	edit.ConnectClicked(func() {
		pop.Popdown()
		en := e
		editor(&en)
	})
	del.ConnectClicked(func() {
		pop.Popdown()
		name := title
		dlg := adw.NewAlertDialog("Delete "+name+"?", "You will not get this account's codes here any more. Make sure you can still sign in to it another way, or you could be locked out.")
		dlg.AddResponse("cancel", "Cancel")
		dlg.AddResponse("delete", "Delete")
		dlg.SetResponseAppearance("delete", adw.ResponseDestructive)
		dlg.SetCloseResponse("cancel")
		dlg.ConnectResponse(func(resp string) {
			if resp == "delete" {
				if err := v.Delete(e.ID); err != nil {
					b.toast("Couldn't delete: "+err.Error(), 3)
				}
				rebuild()
			}
		})
		dlg.Present(b)
	})
	return r
}

// showAuthEntryEditor adds an account (e == nil) or edits one.
func showAuthEntryEditor(b *Browser, e *authEntry, done func()) {
	v := b.app.authenticatorVault()
	d := adw.NewDialog()
	d.SetContentWidth(440)
	if e == nil {
		d.SetTitle("Add Account")
	} else {
		d.SetTitle("Edit Account")
	}
	tv := adw.NewToolbarView()
	hb := adw.NewHeaderBar()
	hb.SetShowEndTitleButtons(false)
	hb.SetShowStartTitleButtons(false)
	cancel := gtk.NewButtonWithLabel("Cancel")
	save := gtk.NewButtonWithLabel("Save")
	save.AddCSSClass("suggested-action")
	hb.PackStart(cancel)
	hb.PackEnd(save)
	tv.AddTopBar(hb)

	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetMarginStart(16)
	box.SetMarginEnd(16)
	box.SetMarginBottom(16)
	box.SetMarginTop(4)
	g := adw.NewPreferencesGroup()
	issuer := adw.NewEntryRow()
	issuer.SetTitle("Name (for example GitHub)")
	account := adw.NewEntryRow()
	account.SetTitle("Account (for example your email)")
	secret := adw.NewPasswordEntryRow()
	secret.SetTitle("Secret key, or an otpauth:// link")
	g.Add(issuer)
	g.Add(account)
	g.Add(secret)
	box.Append(g)
	msg := gtk.NewLabel("")
	msg.SetXAlign(0)
	msg.SetWrap(true)
	msg.AddCSSClass("error")
	box.Append(msg)
	hint := gtk.NewLabel("Paste the secret key the site shows under its QR code, or the whole otpauth:// link. It is saved encrypted on this computer.")
	hint.SetXAlign(0)
	hint.SetWrap(true)
	hint.AddCSSClass("dim-label")
	hint.AddCSSClass("caption")
	box.Append(hint)
	tv.SetContent(box)
	d.SetChild(tv)

	algo, digits, period := "SHA1", 6, 30
	id := ""
	if e != nil {
		issuer.SetText(e.Issuer)
		account.SetText(e.Account)
		secret.SetText(e.Secret)
		n := e.normalized()
		algo, digits, period, id = n.Algorithm, n.Digits, n.Period, e.ID
	}
	// A pasted otpauth:// link fills in everything.
	fill := false
	secret.ConnectChanged(func() {
		if fill {
			return
		}
		if t := strings.TrimSpace(secret.Text()); strings.HasPrefix(strings.ToLower(t), "otpauth://") {
			ne, err := parseOtpauth(t)
			if err != nil {
				msg.SetText(capitalFirst(err.Error()) + ".")
				return
			}
			msg.SetText("")
			fill = true
			secret.SetText(ne.Secret)
			if issuer.Text() == "" {
				issuer.SetText(ne.Issuer)
			}
			if account.Text() == "" {
				account.SetText(ne.Account)
			}
			algo, digits, period = ne.Algorithm, ne.Digits, ne.Period
			fill = false
		}
	})
	cancel.ConnectClicked(func() { d.Close() })
	save.ConnectClicked(func() {
		ne := authEntry{ID: id, Issuer: strings.TrimSpace(issuer.Text()), Account: strings.TrimSpace(account.Text()),
			Secret: strings.TrimSpace(secret.Text()), Algorithm: algo, Digits: digits, Period: period}
		if ne.Issuer == "" && ne.Account == "" {
			msg.SetText("Give the account a name.")
			return
		}
		if err := v.Save(ne); err != nil {
			msg.SetText(capitalFirst(err.Error()) + ".")
			return
		}
		d.Close()
		done()
	})
	d.Present(b)
}

// ---- Settings > Privacy ---------------------------------------------------------------

// authenticatorGroup is the Authenticator part of the Privacy settings: open
// it, and the PIN (off until you turn it on).
func authenticatorGroup(b *Browser) *adw.PreferencesGroup {
	v := b.app.authenticatorVault()
	g := adw.NewPreferencesGroup()
	g.SetTitle("Authenticator")
	g.SetDescription("One-time codes for two-step sign-in, kept encrypted on this computer.")

	open := adw.NewActionRow()
	open.SetTitle("Authenticator")
	open.SetSubtitle("See and add your codes")
	suffixButton(open, "Open", "", func() { showAuthenticator(b) })
	g.Add(open)

	pin := adw.NewSwitchRow()
	pin.SetTitle("Require a PIN")
	pin.SetSubtitle("Off by default. When on, the Authenticator asks for it every time you open it.")
	pin.SetActive(v.hasPIN())
	busy := false
	pin.NotifyProperty("active", func() {
		if busy {
			return
		}
		want := pin.Active()
		if want == v.hasPIN() {
			return
		}
		revert := func() { busy = true; pin.SetActive(!want); busy = false }
		if want {
			setAuthPIN(b, revert)
		} else {
			removeAuthPIN(b, revert)
		}
	})
	g.Add(pin)
	return g
}

// setAuthPIN asks for a new PIN twice and turns the PIN on. cancelled is
// called if it didn't happen.
func setAuthPIN(b *Browser, cancelled func()) {
	v := b.app.authenticatorVault()
	if err := v.Open(); err != nil && err != errLocked {
		b.toast("Couldn't open the Authenticator: "+err.Error(), 3)
		cancelled()
		return
	}
	var ask func(note string)
	ask = func(note string) {
		d := adw.NewAlertDialog("Set a PIN", "Choose at least 4 characters. You'll need it to open your codes, and it can't be recovered if you forget it.")
		if note != "" {
			d.SetBody(note)
		}
		d.AddResponse("cancel", "Cancel")
		d.AddResponse("set", "Turn On")
		d.SetResponseAppearance("set", adw.ResponseSuggested)
		d.SetDefaultResponse("set")
		d.SetCloseResponse("cancel")
		col := gtk.NewBox(gtk.OrientationVertical, 8)
		one := gtk.NewPasswordEntry()
		one.SetShowPeekIcon(true)
		one.SetObjectProperty("placeholder-text", "PIN")
		two := gtk.NewPasswordEntry()
		two.SetObjectProperty("placeholder-text", "PIN again")
		two.SetObjectProperty("activates-default", true)
		col.Append(one)
		col.Append(two)
		d.SetExtraChild(col)
		d.ConnectResponse(func(r string) {
			if r != "set" {
				cancelled()
				return
			}
			if one.Text() != two.Text() {
				ask("The two PINs don't match.")
				return
			}
			if err := v.SetPIN(one.Text()); err != nil {
				ask(capitalFirst(err.Error()) + ".")
				return
			}
			b.toast("PIN turned on", 2)
		})
		d.Present(b)
		one.GrabFocus()
	}
	ask("")
}

// removeAuthPIN checks the PIN and turns it off.
func removeAuthPIN(b *Browser, cancelled func()) {
	v := b.app.authenticatorVault()
	var ask func(note string)
	ask = func(note string) {
		msg := "Enter your PIN to turn it off."
		if note != "" {
			msg = note
		}
		d := adw.NewAlertDialog("Turn off the PIN?", msg)
		d.AddResponse("cancel", "Cancel")
		d.AddResponse("off", "Turn Off")
		d.SetResponseAppearance("off", adw.ResponseDestructive)
		d.SetDefaultResponse("off")
		d.SetCloseResponse("cancel")
		entry := gtk.NewPasswordEntry()
		entry.SetShowPeekIcon(true)
		entry.SetObjectProperty("activates-default", true)
		d.SetExtraChild(entry)
		d.ConnectResponse(func(r string) {
			if r != "off" {
				cancelled()
				return
			}
			if !v.unlocked() {
				if err := v.Unlock(entry.Text()); err != nil {
					ask(capitalFirst(err.Error()) + ".")
					return
				}
			}
			if err := v.RemovePIN(entry.Text()); err != nil {
				ask(capitalFirst(err.Error()) + ".")
				return
			}
			v.Lock()
			b.toast("PIN turned off", 2)
		})
		d.Present(b)
		entry.GrabFocus()
	}
	ask("")
}
