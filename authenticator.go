package main

// An authenticator for one-time codes (TOTP, RFC 6238), like Proton
// Authenticator or Google Authenticator: you add the secret a site gives you
// and this shows the 6-digit code that changes every 30 seconds.
//
// The secrets are kept encrypted (AES-256-GCM). The key lives in the system
// keyring (secret-tool) when there is one, else in a 0600 file. If you turn
// the PIN on (it is off until you do), the key is taken out of the keyring and
// kept only in a form that needs the PIN to open (PBKDF2-HMAC-SHA256, 600,000
// rounds), so the PIN protects the secrets even from someone with your files
// and your logged-in session, not just from someone at your screen. A wrong
// PIN makes you wait, longer each time.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- codes ---------------------------------------------------------------------

type authEntry struct {
	ID        string `json:"id"`
	Issuer    string `json:"issuer"`
	Account   string `json:"account"`
	Secret    string `json:"secret"`    // base32, as the site gave it
	Algorithm string `json:"algorithm"` // SHA1 (default), SHA256, SHA512
	Digits    int    `json:"digits"`    // 6 (default) or 8
	Period    int    `json:"period"`    // seconds, 30 by default
	Created   int64  `json:"created"`
}

func (e authEntry) normalized() authEntry {
	if e.Algorithm == "" {
		e.Algorithm = "SHA1"
	}
	if e.Digits == 0 {
		e.Digits = 6
	}
	if e.Period == 0 {
		e.Period = 30
	}
	return e
}

// decodeSecret reads the base32 secret sites hand out: any case, with or
// without spaces, dashes or padding.
func decodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '=' || r == '\t' || r == '\n' {
			return -1
		}
		return r
	}, s))
	if s == "" {
		return nil, errors.New("the secret is empty")
	}
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil || len(b) == 0 {
		return nil, errors.New("that isn't a valid secret key")
	}
	return b, nil
}

// totpAt is the code for the time window that contains t.
func totpAt(e authEntry, t time.Time) (string, error) {
	e = e.normalized()
	key, err := decodeSecret(e.Secret)
	if err != nil {
		return "", err
	}
	var h func() hash.Hash
	switch strings.ToUpper(e.Algorithm) {
	case "SHA1":
		h = sha1.New
	case "SHA256":
		h = sha256.New
	case "SHA512":
		h = sha512.New
	default:
		return "", errors.New("unsupported algorithm " + e.Algorithm)
	}
	if e.Digits < 6 || e.Digits > 8 || e.Period < 1 {
		return "", errors.New("unsupported digits or period")
	}
	counter := uint64(t.Unix()) / uint64(e.Period)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(h, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < e.Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", e.Digits, bin%mod), nil
}

// secondsLeft is how long the current code lasts.
func (e authEntry) secondsLeft(t time.Time) int {
	p := e.normalized().Period
	return p - int(t.Unix()%int64(p))
}

// groupCode puts a space in the middle: 209117 -> 209 117.
func groupCode(c string) string {
	if len(c) < 6 {
		return c
	}
	h := len(c) / 2
	return c[:h] + " " + c[h:]
}

// parseOtpauth reads an otpauth://totp/... link, the thing a QR code holds.
func parseOtpauth(raw string) (authEntry, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "otpauth" {
		return authEntry{}, errors.New("that isn't an otpauth:// link")
	}
	if u.Host != "totp" {
		return authEntry{}, errors.New("only time-based (TOTP) codes are supported")
	}
	label := strings.TrimPrefix(u.Path, "/")
	label, _ = url.PathUnescape(label)
	q := u.Query()
	e := authEntry{Secret: q.Get("secret"), Algorithm: strings.ToUpper(q.Get("algorithm")), Issuer: q.Get("issuer")}
	if i := strings.Index(label, ":"); i >= 0 {
		if e.Issuer == "" {
			e.Issuer = strings.TrimSpace(label[:i])
		}
		label = label[i+1:]
	}
	e.Account = strings.TrimSpace(label)
	if v, err := strconv.Atoi(q.Get("digits")); err == nil {
		e.Digits = v
	}
	if v, err := strconv.Atoi(q.Get("period")); err == nil {
		e.Period = v
	}
	e = e.normalized()
	if _, err := decodeSecret(e.Secret); err != nil {
		return authEntry{}, err
	}
	if _, err := totpAt(e, time.Now()); err != nil {
		return authEntry{}, err
	}
	return e, nil
}

// ---- keys ------------------------------------------------------------------------

// keyBackend is where the vault's key is kept when there is no PIN.
type keyBackend interface {
	load() ([]byte, error)
	save(key []byte) error
	remove() error
}

type keyringBackend struct{ kind string }

func (k keyringBackend) file() string { return filepath.Join(dataDir(), k.kind+".key") }

func (k keyringBackend) load() ([]byte, error) {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		out, err := exec.Command("secret-tool", "lookup", "application", appName, "kind", k.kind).Output()
		if err == nil {
			if key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out))); err == nil && len(key) == 32 {
				return key, nil
			}
		}
	}
	if b, err := os.ReadFile(k.file()); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, os.ErrNotExist
}

func (k keyringBackend) save(key []byte) error {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		cmd := exec.Command("secret-tool", "store", "--label=Fasty "+k.kind, "application", appName, "kind", k.kind)
		cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(key))
		if cmd.Run() == nil {
			os.Remove(k.file())
			return nil
		}
	}
	return os.WriteFile(k.file(), key, 0o600)
}

func (k keyringBackend) remove() error {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		exec.Command("secret-tool", "clear", "application", appName, "kind", k.kind).Run()
	}
	os.Remove(k.file())
	return nil
}

// ---- the vault -----------------------------------------------------------------------

const pinRounds = 600000

type pinBlock struct {
	Salt    string `json:"salt"`    // base64url
	Wrapped string `json:"wrapped"` // base64url: the vault key, sealed with the PIN's key
	Rounds  int    `json:"rounds"`
	Fails   int    `json:"fails"`
	Until   int64  `json:"until"` // no tries before this time (unix)
}

type vaultFile struct {
	Version int       `json:"version"`
	Data    string    `json:"data"` // base64url(nonce || AES-GCM(key, JSON entries))
	PIN     *pinBlock `json:"pin,omitempty"`
}

type authVault struct {
	mu      sync.Mutex
	path    string
	keys    keyBackend
	file    vaultFile
	key     []byte // the vault key, only while unlocked
	entries []authEntry
}

var errLocked = errors.New("locked")

func newAuthVault(path string, keys keyBackend) *authVault {
	v := &authVault{path: path, keys: keys}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &v.file)
	}
	return v
}

func (v *authVault) hasPIN() bool { v.mu.Lock(); defer v.mu.Unlock(); return v.file.PIN != nil }

func (v *authVault) unlocked() bool { v.mu.Lock(); defer v.mu.Unlock(); return v.key != nil }

func gcmSeal(key, aad, plain []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return gcm.Seal(nonce, nonce, plain, aad), nil
}

func gcmOpen(key, aad, sealed []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("short")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], aad)
}

func (v *authVault) saveLocked() error {
	plain, _ := json.Marshal(v.entries)
	sealed, err := gcmSeal(v.key, []byte("fasty-authenticator"), plain)
	if err != nil {
		return err
	}
	v.file.Version = 1
	v.file.Data = b64.EncodeToString(sealed)
	b, _ := json.MarshalIndent(v.file, "", "  ")
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

func (v *authVault) loadEntriesLocked() error {
	v.entries = nil
	if v.file.Data == "" {
		return nil
	}
	sealed, err := b64.DecodeString(v.file.Data)
	if err != nil {
		return err
	}
	plain, err := gcmOpen(v.key, []byte("fasty-authenticator"), sealed)
	if err != nil {
		return errors.New("the saved codes couldn't be opened")
	}
	return json.Unmarshal(plain, &v.entries)
}

// Open unlocks a vault that has no PIN, creating it on first use. With a PIN
// it does nothing: use Unlock.
func (v *authVault) Open() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key != nil {
		return nil
	}
	if v.file.PIN != nil {
		return errLocked
	}
	key, err := v.keys.load()
	if err != nil {
		if v.file.Data != "" {
			return errors.New("the key to the saved codes is missing")
		}
		key = make([]byte, 32)
		rand.Read(key)
		if err := v.keys.save(key); err != nil {
			return err
		}
	}
	v.key = key
	return v.loadEntriesLocked()
}

// WaitFor says how many seconds must pass before the PIN may be tried again.
func (v *authVault) WaitFor() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file.PIN == nil {
		return 0
	}
	if d := v.file.PIN.Until - time.Now().Unix(); d > 0 {
		return int(d)
	}
	return 0
}

func pinKey(pin string, salt []byte, rounds int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, pin, salt, rounds, 32)
}

// Unlock opens a vault that has a PIN. A wrong PIN costs a wait.
func (v *authVault) Unlock(pin string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.file.PIN
	if p == nil {
		return errors.New("no PIN is set")
	}
	if d := p.Until - time.Now().Unix(); d > 0 {
		return fmt.Errorf("too many tries: wait %d seconds", d)
	}
	salt, _ := b64.DecodeString(p.Salt)
	wrapped, _ := b64.DecodeString(p.Wrapped)
	pk, err := pinKey(pin, salt, p.Rounds)
	if err == nil {
		var key []byte
		if key, err = gcmOpen(pk, []byte("fasty-authenticator-pin"), wrapped); err == nil {
			v.key = key
			p.Fails, p.Until = 0, 0
			v.persistPINLocked()
			return v.loadEntriesLocked()
		}
	}
	p.Fails++
	if p.Fails >= 3 {
		wait := 5 << uint(min(p.Fails-3, 10)) // 5, 10, 20 ... seconds
		p.Until = time.Now().Unix() + int64(wait)
	}
	v.persistPINLocked()
	return errors.New("wrong PIN")
}

func (v *authVault) persistPINLocked() {
	b, _ := json.MarshalIndent(v.file, "", "  ")
	os.WriteFile(v.path, b, 0o600)
}

// Lock forgets the key; opening again needs the PIN, if there is one.
func (v *authVault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i := range v.key {
		v.key[i] = 0
	}
	v.key = nil
	v.entries = nil
}

// SetPIN turns the PIN on: the vault key leaves the keyring and is kept only
// sealed with the PIN. The vault must be open.
func (v *authVault) SetPIN(pin string) error {
	if len(pin) < 4 {
		return errors.New("a PIN needs at least 4 characters")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return errLocked
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	pk, err := pinKey(pin, salt, pinRounds)
	if err != nil {
		return err
	}
	wrapped, err := gcmSeal(pk, []byte("fasty-authenticator-pin"), v.key)
	if err != nil {
		return err
	}
	v.file.PIN = &pinBlock{Salt: b64.EncodeToString(salt), Wrapped: b64.EncodeToString(wrapped), Rounds: pinRounds}
	if err := v.saveLocked(); err != nil {
		v.file.PIN = nil
		return err
	}
	v.keys.remove()
	return nil
}

// RemovePIN turns the PIN off, after checking it: the key goes back to the keyring.
func (v *authVault) RemovePIN(pin string) error {
	v.mu.Lock()
	p := v.file.PIN
	v.mu.Unlock()
	if p == nil {
		return nil
	}
	// Check the PIN the same way a fresh unlock would, without disturbing the open vault.
	v.mu.Lock()
	defer v.mu.Unlock()
	if d := p.Until - time.Now().Unix(); d > 0 {
		return fmt.Errorf("too many tries: wait %d seconds", d)
	}
	salt, _ := b64.DecodeString(p.Salt)
	wrapped, _ := b64.DecodeString(p.Wrapped)
	pk, err := pinKey(pin, salt, p.Rounds)
	if err == nil {
		_, err = gcmOpen(pk, []byte("fasty-authenticator-pin"), wrapped)
	}
	if err != nil {
		p.Fails++
		if p.Fails >= 3 {
			p.Until = time.Now().Unix() + int64(5<<uint(min(p.Fails-3, 10)))
		}
		v.persistPINLocked()
		return errors.New("wrong PIN")
	}
	if v.key == nil {
		return errLocked
	}
	if err := v.keys.save(v.key); err != nil {
		return err
	}
	v.file.PIN = nil
	return v.saveLocked()
}

// ---- entries ---------------------------------------------------------------------------

func (v *authVault) List() []authEntry {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := append([]authEntry(nil), v.entries...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

func (v *authVault) Save(e authEntry) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return errLocked
	}
	e = e.normalized()
	if _, err := totpAt(e, time.Now()); err != nil {
		return err
	}
	if e.ID == "" {
		id := make([]byte, 8)
		rand.Read(id)
		e.ID = b64.EncodeToString(id)
		e.Created = time.Now().UnixNano()
		v.entries = append(v.entries, e)
	} else {
		found := false
		for i := range v.entries {
			if v.entries[i].ID == e.ID {
				e.Created = v.entries[i].Created
				v.entries[i] = e
				found = true
			}
		}
		if !found {
			return errors.New("that entry is gone")
		}
	}
	return v.saveLocked()
}

func (v *authVault) Delete(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil {
		return errLocked
	}
	for i, e := range v.entries {
		if e.ID == id {
			v.entries = append(v.entries[:i], v.entries[i+1:]...)
			break
		}
	}
	return v.saveLocked()
}

// authenticatorVault is the one vault the app uses.
func (a *App) authenticatorVault() *authVault {
	if a.authVault == nil {
		a.authVault = newAuthVault(filepath.Join(dataDir(), "authenticator.json"), keyringBackend{kind: "authenticator"})
	}
	return a.authVault
}
