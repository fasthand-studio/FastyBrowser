package main

// Passkeys (WebAuthn). WebKitGTK has no WebAuthn, so pages that look for
// navigator.credentials / PublicKeyCredential find nothing and hide their
// "passkey" and sign-in options. assets/js/webauthn.js defines the standard
// API and hands each request to this file, which acts as a built-in
// authenticator: it makes and keeps ES256 (P-256) passkeys on this computer
// and signs sign-in challenges with them, after asking you.
//
// What it guarantees, and doesn't:
//   - The site's origin comes from the tab's address, never from the page's
//     own words, and the passkey is only ever made or used for that site.
//   - Every make and every sign-in asks first. Nothing happens silently.
//   - Private keys are encrypted (AES-256-GCM). The key for that lives in the
//     system keyring (secret-tool) when there is one, else in a 0600 file.
//   - Passkeys stay on this computer: they don't sync, and your phone's or
//     Google's passkeys aren't visible here.
//   - Only ES256 (alg -7), no attestation, no conditional (autofill) UI.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

var b64 = base64.RawURLEncoding

// ---- storage -------------------------------------------------------------

type passkey struct {
	ID          string `json:"id"` // credential id, base64url
	RPID        string `json:"rp_id"`
	UserHandle  string `json:"user_handle"` // base64url
	UserName    string `json:"user_name"`
	DisplayName string `json:"display_name"`
	Key         string `json:"key"` // base64url(nonce || AES-GCM(PKCS#8 private key))
	Created     int64  `json:"created"`
}

type passkeyStore struct {
	mu   sync.Mutex
	path string
	list []passkey
}

func passkeys() *passkeyStore {
	s := &passkeyStore{path: filepath.Join(dataDir(), "passkeys.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		json.Unmarshal(b, &s.list)
	}
	return s
}

func (s *passkeyStore) save() error {
	b, _ := json.MarshalIndent(s.list, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// masterKey is the key passkeys are encrypted with: in the system keyring if
// there is one, otherwise a 0600 file next to them.
func masterKey() ([]byte, error) {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		out, err := exec.Command("secret-tool", "lookup", "application", appName, "kind", "passkeys").Output()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			if k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out))); err == nil && len(k) == 32 {
				return k, nil
			}
		}
		k := make([]byte, 32)
		rand.Read(k)
		cmd := exec.Command("secret-tool", "store", "--label=Fasty passkeys", "application", appName, "kind", "passkeys")
		cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(k))
		if err := cmd.Run(); err == nil {
			return k, nil
		}
		log.Printf("fasty: passkeys: the keyring wouldn't take the key, using a file")
	}
	p := filepath.Join(dataDir(), "passkeys.key")
	if b, err := os.ReadFile(p); err == nil && len(b) == 32 {
		return b, nil
	}
	k := make([]byte, 32)
	rand.Read(k)
	return k, os.WriteFile(p, k, 0o600)
}

func sealKey(master, id, plain []byte) (string, error) {
	blk, err := aes.NewCipher(master)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return b64.EncodeToString(gcm.Seal(nonce, nonce, plain, id)), nil
}

func openKey(master, id []byte, sealed string) ([]byte, error) {
	raw, err := b64.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("short")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], id)
}

// ---- CBOR (just what a passkey needs) -----------------------------------------

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	case n < 65536:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	default:
		b := []byte{major<<5 | 26, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], uint32(n))
		return b
	}
}

func cborInt(v int) []byte {
	if v >= 0 {
		return cborHead(0, uint64(v))
	}
	return cborHead(1, uint64(-1-v))
}
func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }
func cborText(s string) []byte  { return append(cborHead(3, uint64(len(s))), s...) }
func cborMap(pairs ...[]byte) []byte {
	out := cborHead(5, uint64(len(pairs)/2))
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

// ---- the request from the page ---------------------------------------------

type waRequest struct {
	Op        string `json:"op"` // create | get | cancel
	ID        int    `json:"id"`
	RPID      string `json:"rpId"`
	RPName    string `json:"rpName"`
	Challenge string `json:"challenge"` // base64url
	User      struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"user"`
	Algs    []int    `json:"algs"`
	Exclude []string `json:"exclude"`
	Allow   []string `json:"allow"`
	UV      string   `json:"userVerification"`
}

// webauthnOrigin is the page's origin, taken from where the tab really is.
func webauthnOrigin(raw string) (origin, host string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	host = strings.ToLower(u.Hostname())
	local := host == "localhost" || strings.HasSuffix(host, ".localhost")
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", "", false
	}
	return u.Scheme + "://" + u.Host, host, true
}

// A short list of two-part public suffixes, since there is no suffix list here.
var twoPartSuffix = map[string]bool{"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "com.au": true,
	"net.au": true, "org.au": true, "co.in": true, "co.jp": true, "com.br": true, "com.bd": true, "co.nz": true,
	"co.za": true, "com.cn": true, "com.tr": true, "com.mx": true, "github.io": true, "herokuapp.com": true,
	"pages.dev": true, "vercel.app": true, "netlify.app": true, "blogspot.com": true}

func rpIDAllowed(host, rp string) bool {
	rp = strings.ToLower(rp)
	if rp == "" || rp != strings.TrimSpace(rp) {
		return false
	}
	if rp == host {
		return true
	}
	return strings.HasSuffix(host, "."+rp) && strings.Contains(rp, ".") && !twoPartSuffix[rp]
}

type waError struct{ name, message string }

func (b *Browser) webauthn(t *Tab, body string) {
	var r waRequest
	if json.Unmarshal([]byte(body), &r) != nil {
		return
	}
	if r.Op == "cancel" {
		b.passkeyCancel(t, r.ID)
		return
	}
	reply := func(res map[string]any, e *waError) {
		out := map[string]any{"ok": e == nil}
		if e != nil {
			out["error"] = map[string]string{"name": e.name, "message": e.message}
		} else {
			out["result"] = res
		}
		js, _ := json.Marshal(out)
		t.run(fmt.Sprintf("window.__fastyWA && window.__fastyWA.done(%d, %s); 0", r.ID, js), nil)
	}
	uri := ""
	if t.view != nil {
		uri = t.view.URI()
	}
	origin, host, ok := webauthnOrigin(uri)
	if !ok {
		reply(nil, &waError{"SecurityError", "Passkeys need a secure (https) page."})
		return
	}
	if r.RPID == "" {
		r.RPID = host
	}
	if !rpIDAllowed(host, r.RPID) {
		reply(nil, &waError{"SecurityError", "The relying party ID is not allowed for this site."})
		return
	}
	challenge, err := b64.DecodeString(r.Challenge)
	if err != nil || len(challenge) == 0 {
		reply(nil, &waError{"TypeError", "Bad challenge."})
		return
	}
	switch r.Op {
	case "create":
		b.passkeyCreate(t, r, origin, challenge, reply)
	case "get":
		b.passkeyGet(t, r, origin, challenge, reply)
	}
}

func clientData(typ, origin string, challenge []byte) []byte {
	cd, _ := json.Marshal(map[string]any{"type": typ, "challenge": b64.EncodeToString(challenge), "origin": origin, "crossOrigin": false})
	return cd
}

func uvFlag(uv string) byte {
	if uv == "discouraged" {
		return 0
	}
	return 0x04 // you were asked and agreed; see the top of this file
}

// ---- asking -------------------------------------------------------------------

// passkeyPrompt is the one question being asked. Tests answer it through it.
type passkeyPrompt struct {
	tab     *Tab
	id      int
	dialog  *adw.AlertDialog
	respond func(resp string, pick int)
}

func (b *Browser) passkeyCancel(t *Tab, id int) {
	if p := b.passkeyAsk; p != nil && p.tab == t && p.id == id {
		p.respond("deny", 0)
		p.dialog.ForceClose()
	}
}

// askPasskey shows the question; done gets "allow" and the chosen row, or "deny".
func (b *Browser) askPasskey(t *Tab, id int, heading, body string, choices []string, done func(allow bool, pick int)) {
	if b.passkeyAsk != nil {
		done(false, 0)
		return
	}
	d := adw.NewAlertDialog(heading, body)
	d.AddResponse("deny", "Cancel")
	d.AddResponse("allow", "Continue")
	d.SetResponseAppearance("allow", adw.ResponseSuggested)
	d.SetDefaultResponse("allow")
	d.SetCloseResponse("deny")
	var drop *gtk.DropDown
	if len(choices) > 1 {
		drop = gtk.NewDropDown(gtk.NewStringList(choices), nil)
		d.SetExtraChild(drop)
	}
	finished := false
	respond := func(resp string, pick int) {
		if finished {
			return
		}
		finished = true
		b.passkeyAsk = nil
		done(resp == "allow", pick)
	}
	b.passkeyAsk = &passkeyPrompt{tab: t, id: id, dialog: d, respond: respond}
	d.ConnectResponse(func(r string) {
		pick := 0
		if drop != nil {
			pick = int(drop.Selected())
		}
		respond(r, pick)
	})
	d.Present(b)
}

// ---- make a passkey -------------------------------------------------------------

func (b *Browser) passkeyCreate(t *Tab, r waRequest, origin string, challenge []byte, reply func(map[string]any, *waError)) {
	es256 := false
	for _, a := range r.Algs {
		if a == -7 {
			es256 = true
		}
	}
	if len(r.Algs) > 0 && !es256 {
		reply(nil, &waError{"NotSupportedError", "This browser's passkeys use ES256 only."})
		return
	}
	userHandle, err := b64.DecodeString(r.User.ID)
	if err != nil || len(userHandle) == 0 {
		reply(nil, &waError{"TypeError", "Bad user id."})
		return
	}
	store := b.app.passkeys
	store.mu.Lock()
	for _, k := range store.list {
		if k.RPID != r.RPID {
			continue
		}
		for _, ex := range r.Exclude {
			if ex == k.ID {
				store.mu.Unlock()
				reply(nil, &waError{"InvalidStateError", "A passkey for this account already exists here."})
				return
			}
		}
	}
	store.mu.Unlock()

	who := r.User.Name
	if who == "" {
		who = r.User.DisplayName
	}
	b.askPasskey(t, r.ID, "Create a passkey for "+r.RPID+"?",
		"It will be saved on this computer for "+who+". You'll use it to sign in to "+r.RPID+" without a password.",
		nil, func(allow bool, _ int) {
			if !allow {
				reply(nil, &waError{"NotAllowedError", "The operation was cancelled."})
				return
			}
			res, e := store.makeCredential(r, origin, challenge, userHandle)
			reply(res, e)
		})
}

func (s *passkeyStore) makeCredential(r waRequest, origin string, challenge, userHandle []byte) (map[string]any, *waError) {
	master, err := masterKey()
	if err != nil {
		return nil, &waError{"UnknownError", "Couldn't get the key that protects passkeys."}
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, &waError{"UnknownError", err.Error()}
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, &waError{"UnknownError", err.Error()}
	}
	id := make([]byte, 32)
	rand.Read(id)
	sealed, err := sealKey(master, id, der)
	if err != nil {
		return nil, &waError{"UnknownError", err.Error()}
	}
	s.mu.Lock()
	s.list = append(s.list, passkey{ID: b64.EncodeToString(id), RPID: r.RPID, UserHandle: b64.EncodeToString(userHandle),
		UserName: r.User.Name, DisplayName: r.User.DisplayName, Key: sealed, Created: time.Now().Unix()})
	err = s.save()
	s.mu.Unlock()
	if err != nil {
		return nil, &waError{"UnknownError", "Couldn't save the passkey."}
	}

	x, y := make([]byte, 32), make([]byte, 32)
	priv.PublicKey.X.FillBytes(x)
	priv.PublicKey.Y.FillBytes(y)
	cose := cborMap(cborInt(1), cborInt(2), cborInt(3), cborInt(-7), cborInt(-1), cborInt(1),
		cborInt(-2), cborBytes(x), cborInt(-3), cborBytes(y))
	rpHash := sha256.Sum256([]byte(r.RPID))
	auth := append([]byte{}, rpHash[:]...)
	auth = append(auth, 0x01|uvFlag(r.UV)|0x40, 0, 0, 0, 0)
	auth = append(auth, make([]byte, 16)...) // AAGUID: none
	auth = append(auth, byte(len(id)>>8), byte(len(id)))
	auth = append(auth, id...)
	auth = append(auth, cose...)
	att := cborMap(cborText("fmt"), cborText("none"), cborText("attStmt"), cborMap(), cborText("authData"), cborBytes(auth))
	spki, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	return map[string]any{
		"id":                b64.EncodeToString(id),
		"clientDataJSON":    b64.EncodeToString(clientData("webauthn.create", origin, challenge)),
		"attestationObject": b64.EncodeToString(att),
		"authenticatorData": b64.EncodeToString(auth),
		"publicKey":         b64.EncodeToString(spki),
		"publicKeyAlg":      -7,
	}, nil
}

// ---- sign in -------------------------------------------------------------------

func (b *Browser) passkeyGet(t *Tab, r waRequest, origin string, challenge []byte, reply func(map[string]any, *waError)) {
	store := b.app.passkeys
	store.mu.Lock()
	var found []passkey
	for _, k := range store.list {
		if k.RPID != r.RPID {
			continue
		}
		if len(r.Allow) > 0 {
			ok := false
			for _, a := range r.Allow {
				ok = ok || a == k.ID
			}
			if !ok {
				continue
			}
		}
		found = append(found, k)
	}
	store.mu.Unlock()
	if len(found) == 0 {
		reply(nil, &waError{"NotAllowedError", "No passkey for this site on this computer."})
		return
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Created > found[j].Created })
	names := make([]string, len(found))
	for i, k := range found {
		names[i] = k.UserName
		if names[i] == "" {
			names[i] = k.DisplayName
		}
		if names[i] == "" {
			names[i] = "Passkey"
		}
	}
	body := "Sign in as " + names[0] + "."
	if len(found) > 1 {
		body = "Choose the account to sign in with."
	}
	b.askPasskey(t, r.ID, "Sign in to "+r.RPID+" with a passkey?", body, names, func(allow bool, pick int) {
		if !allow || pick < 0 || pick >= len(found) {
			reply(nil, &waError{"NotAllowedError", "The operation was cancelled."})
			return
		}
		res, e := store.assert(found[pick], r, origin, challenge)
		reply(res, e)
	})
}

func (s *passkeyStore) assert(k passkey, r waRequest, origin string, challenge []byte) (map[string]any, *waError) {
	master, err := masterKey()
	if err != nil {
		return nil, &waError{"UnknownError", "Couldn't get the key that protects passkeys."}
	}
	id, _ := b64.DecodeString(k.ID)
	der, err := openKey(master, id, k.Key)
	if err != nil {
		return nil, &waError{"UnknownError", "Couldn't unlock this passkey."}
	}
	pk, err := x509.ParsePKCS8PrivateKey(der)
	priv, ok := pk.(*ecdsa.PrivateKey)
	if err != nil || !ok {
		return nil, &waError{"UnknownError", "This passkey is damaged."}
	}
	rpHash := sha256.Sum256([]byte(k.RPID))
	auth := append([]byte{}, rpHash[:]...)
	auth = append(auth, 0x01|uvFlag(r.UV), 0, 0, 0, 0)
	cd := clientData("webauthn.get", origin, challenge)
	cdHash := sha256.Sum256(cd)
	sum := sha256.Sum256(append(append([]byte{}, auth...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, sum[:])
	if err != nil {
		return nil, &waError{"UnknownError", err.Error()}
	}
	return map[string]any{
		"id":                k.ID,
		"clientDataJSON":    b64.EncodeToString(cd),
		"authenticatorData": b64.EncodeToString(auth),
		"signature":         b64.EncodeToString(sig),
		"userHandle":        k.UserHandle,
	}, nil
}
