package main

// Import and export for the Authenticator.
//
// Export writes either a text file of otpauth:// links, one per line (the
// format most authenticator apps can read, and in the clear, so anyone with
// the file can use your codes), or the same text locked with a password you
// choose (AES-256-GCM, key from PBKDF2-HMAC-SHA256).
//
// Import reads: otpauth:// links (a text file, or inside Proton Authenticator
// and Bitwarden JSON exports), Proton Authenticator's password-locked backups,
// Aegis and 2FAS unencrypted JSON, Google Authenticator "export accounts" links
// (otpauth-migration://), and this app's own password-locked files.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var errNeedPassword = errors.New("this file is locked with a password")

// ---- export ----------------------------------------------------------------------------

func entryURI(e authEntry) string {
	e = e.normalized()
	label := url.PathEscape(e.Account)
	if e.Issuer != "" {
		label = url.PathEscape(e.Issuer) + ":" + label
	}
	q := url.Values{}
	q.Set("secret", strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "").Replace(e.Secret)))
	if e.Issuer != "" {
		q.Set("issuer", e.Issuer)
	}
	q.Set("algorithm", e.Algorithm)
	q.Set("digits", strconv.Itoa(e.Digits))
	q.Set("period", strconv.Itoa(e.Period))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

func exportPlain(entries []authEntry) []byte {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(entryURI(e))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

type exportEnvelope struct {
	Format string `json:"fasty_authenticator_export"`
	Salt   string `json:"salt"`
	Rounds int    `json:"rounds"`
	Data   string `json:"data"`
}

func exportLocked(entries []authEntry, password string) ([]byte, error) {
	if len(password) < 6 {
		return nil, errors.New("use a password of at least 6 characters")
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	key, err := pbkdf2.Key(sha256.New, password, salt, pinRounds, 32)
	if err != nil {
		return nil, err
	}
	sealed, err := gcmSeal(key, []byte("fasty-authenticator-export"), exportPlain(entries))
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(exportEnvelope{"1", b64.EncodeToString(salt), pinRounds, b64.EncodeToString(sealed)}, "", "  ")
}

// ---- import ----------------------------------------------------------------------------

// parseImport reads whatever file it is given into accounts. password is only
// used for this app's own locked files.
func parseImport(data []byte, password string) ([]authEntry, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, errors.New("the file is empty")
	}
	var out []authEntry
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		var root any
		if err := json.Unmarshal([]byte(text), &root); err != nil {
			return nil, errors.New("that file isn't valid JSON")
		}
		if m, ok := root.(map[string]any); ok {
			if _, locked := m["fasty_authenticator_export"]; locked {
				return openLockedExport([]byte(text), password)
			}
			if isProtonLocked(m) {
				return openProtonLocked([]byte(text), password)
			}
			if es, handled, err := importAegis(m); handled {
				return es, err
			}
			if es, handled, err := importTwoFAS(m); handled {
				return es, err
			}
		}
		walkStrings(root, func(path string, s string) { out = append(out, entriesFromText(s)...) })
		out = append(out, bitwardenBare(root)...)
	} else {
		out = entriesFromText(text)
	}
	if len(out) == 0 {
		return nil, errors.New("no accounts were found in that file")
	}
	return out, nil
}

func openLockedExport(raw []byte, password string) ([]authEntry, error) {
	var env exportEnvelope
	if json.Unmarshal(raw, &env) != nil {
		return nil, errors.New("that file is damaged")
	}
	if password == "" {
		return nil, errNeedPassword
	}
	salt, err1 := b64.DecodeString(env.Salt)
	sealed, err2 := b64.DecodeString(env.Data)
	if err1 != nil || err2 != nil || env.Rounds < 100000 || env.Rounds > 5000000 {
		return nil, errors.New("that file is damaged")
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, env.Rounds, 32)
	if err != nil {
		return nil, err
	}
	plain, err := gcmOpen(key, []byte("fasty-authenticator-export"), sealed)
	if err != nil {
		return nil, errors.New("wrong password")
	}
	es := entriesFromText(string(plain))
	if len(es) == 0 {
		return nil, errors.New("no accounts were found in that file")
	}
	return es, nil
}

// entriesFromText finds otpauth:// and otpauth-migration:// links in text.
func entriesFromText(s string) []authEntry {
	var out []authEntry
	for _, field := range strings.Fields(s) {
		low := strings.ToLower(field)
		switch {
		case strings.HasPrefix(low, "otpauth://"):
			if e, err := parseOtpauth(field); err == nil {
				out = append(out, e)
			}
		case strings.HasPrefix(low, "otpauth-migration://"):
			if es, err := decodeMigration(field); err == nil {
				out = append(out, es...)
			}
		}
	}
	return out
}

func walkStrings(v any, fn func(path, s string)) {
	switch x := v.(type) {
	case string:
		fn("", x)
	case []any:
		for _, e := range x {
			walkStrings(e, fn)
		}
	case map[string]any:
		for _, e := range x {
			walkStrings(e, fn)
		}
	}
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func num(m map[string]any, k string) int {
	switch n := m[k].(type) {
	case float64:
		return int(n)
	case string:
		v, _ := strconv.Atoi(n)
		return v
	}
	return 0
}

func maps(v any) []map[string]any {
	var out []map[string]any
	if l, ok := v.([]any); ok {
		for _, e := range l {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// importAegis reads Aegis's unencrypted export.
func importAegis(root map[string]any) ([]authEntry, bool, error) {
	db, isMap := root["db"].(map[string]any)
	if _, isStr := root["db"].(string); isStr {
		return nil, true, errors.New("that Aegis backup is encrypted: export it from Aegis without encryption")
	}
	if !isMap {
		return nil, false, nil
	}
	var out []authEntry
	for _, it := range maps(db["entries"]) {
		if t := strings.ToLower(str(it, "type")); t != "" && t != "totp" {
			continue
		}
		info, _ := it["info"].(map[string]any)
		e := authEntry{Issuer: str(it, "issuer"), Account: str(it, "name"), Secret: str(info, "secret"),
			Algorithm: strings.ToUpper(str(info, "algo")), Digits: num(info, "digits"), Period: num(info, "period")}
		if _, err := totpAt(e.normalized(), time.Now()); err == nil {
			out = append(out, e.normalized())
		}
	}
	return out, true, nil
}

// importTwoFAS reads 2FAS's unencrypted export.
func importTwoFAS(root map[string]any) ([]authEntry, bool, error) {
	svc, ok := root["services"]
	if !ok {
		if _, enc := root["servicesEncrypted"]; enc {
			return nil, true, errors.New("that 2FAS backup is encrypted: export it from 2FAS without a password")
		}
		return nil, false, nil
	}
	var out []authEntry
	for _, it := range maps(svc) {
		otp, _ := it["otp"].(map[string]any)
		if t := strings.ToUpper(str(otp, "tokenType")); t != "" && t != "TOTP" {
			continue
		}
		e := authEntry{Issuer: str(otp, "issuer"), Account: str(otp, "account"), Secret: str(it, "secret"),
			Algorithm: strings.ToUpper(str(otp, "algorithm")), Digits: num(otp, "digits"), Period: num(otp, "period")}
		if e.Issuer == "" {
			e.Issuer = str(it, "name")
		}
		if _, err := totpAt(e.normalized(), time.Now()); err == nil {
			out = append(out, e.normalized())
		}
	}
	return out, true, nil
}

// bitwardenBare finds Bitwarden items whose "totp" is a bare secret, not a link.
func bitwardenBare(root any) []authEntry {
	m, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	var out []authEntry
	for _, it := range maps(m["items"]) {
		login, _ := it["login"].(map[string]any)
		secret := strings.TrimSpace(str(login, "totp"))
		if secret == "" || strings.Contains(secret, "://") {
			continue
		}
		e := authEntry{Issuer: str(it, "name"), Account: str(login, "username"), Secret: secret}.normalized()
		if _, err := totpAt(e, time.Now()); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// ---- Google Authenticator "export accounts" ---------------------------------------------------

// decodeMigration reads otpauth-migration://offline?data=... (a protobuf).
func decodeMigration(link string) ([]authEntry, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	data := u.Query().Get("data")
	if data == "" {
		return nil, errors.New("that link has no data")
	}
	var raw []byte
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if raw, err = enc.DecodeString(data); err == nil {
			break
		}
	}
	if err != nil {
		return nil, errors.New("that link is damaged")
	}
	var out []authEntry
	for _, f := range pbFields(raw) {
		if f.num != 1 || f.bytes == nil {
			continue
		}
		var e authEntry
		var typ uint64 = 2
		algo, digits := uint64(1), uint64(1)
		var name, issuer string
		var secret []byte
		for _, g := range pbFields(f.bytes) {
			switch g.num {
			case 1:
				secret = g.bytes
			case 2:
				name = string(g.bytes)
			case 3:
				issuer = string(g.bytes)
			case 4:
				algo = g.varint
			case 5:
				digits = g.varint
			case 6:
				typ = g.varint
			}
		}
		if typ != 2 || len(secret) == 0 { // time-based only
			continue
		}
		e.Secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
		e.Issuer = issuer
		e.Account = name
		if i := strings.Index(name, ":"); i >= 0 {
			if e.Issuer == "" {
				e.Issuer = strings.TrimSpace(name[:i])
			}
			e.Account = strings.TrimSpace(name[i+1:])
		}
		switch algo {
		case 2:
			e.Algorithm = "SHA256"
		case 3:
			e.Algorithm = "SHA512"
		case 4:
			continue // MD5: not supported
		}
		if digits == 2 {
			e.Digits = 8
		}
		out = append(out, e.normalized())
	}
	if len(out) == 0 {
		return nil, errors.New("no time-based accounts in that link")
	}
	return out, nil
}

type pbField struct {
	num    int
	varint uint64
	bytes  []byte
}

// pbFields reads the top level of a protobuf message: varints and
// length-delimited fields, which is all this format uses.
func pbFields(b []byte) []pbField {
	var out []pbField
	i := 0
	readVarint := func() (uint64, bool) {
		var v uint64
		for shift := uint(0); i < len(b) && shift < 64; shift += 7 {
			c := b[i]
			i++
			v |= uint64(c&0x7f) << shift
			if c&0x80 == 0 {
				return v, true
			}
		}
		return 0, false
	}
	for i < len(b) {
		tag, ok := readVarint()
		if !ok {
			break
		}
		f := pbField{num: int(tag >> 3)}
		switch tag & 7 {
		case 0:
			if f.varint, ok = readVarint(); !ok {
				return out
			}
		case 2:
			n, ok := readVarint()
			if !ok || uint64(len(b)-i) < n {
				return out
			}
			f.bytes = b[i : i+int(n)]
			i += int(n)
		case 1:
			i += 8
		case 5:
			i += 4
		default:
			return out
		}
		out = append(out, f)
	}
	return out
}

// mergeImported adds accounts to the vault, skipping ones it already has.
// It returns how many were added and how many were already there.
func mergeImported(v *authVault, in []authEntry) (added, skipped int, err error) {
	have := map[string]bool{}
	key := func(e authEntry) string {
		return strings.ToLower(e.Issuer + "\x00" + e.Account + "\x00" + strings.ToUpper(strings.NewReplacer(" ", "", "=", "").Replace(e.Secret)))
	}
	for _, e := range v.List() {
		have[key(e)] = true
	}
	for _, e := range in {
		if have[key(e)] {
			skipped++
			continue
		}
		e.ID = ""
		if err := v.Save(e); err != nil {
			return added, skipped, fmt.Errorf("%s: %w", e.Issuer, err)
		}
		have[key(e)] = true
		added++
	}
	return added, skipped, nil
}

// ---- Proton Authenticator's password-locked backup ------------------------------------------------
//
// {"version":1,"salt":"<base64, 16 bytes>","content":"<base64>"}
//   key  = Argon2id (v1.3, 19 MiB, 2 passes, 1 lane) of the password and salt, 32 bytes
//   content = nonce (12 bytes) || AES-256-GCM(key, JSON, aad "proton.authenticator.export.v1")
// as in protonpass/proton-pass-common, proton-authenticator/src/entry/password_exporter.rs.

func isProtonLocked(m map[string]any) bool {
	_, hasSalt := m["salt"].(string)
	_, hasContent := m["content"].(string)
	_, hasEntries := m["entries"]
	return hasSalt && hasContent && !hasEntries
}

func protonKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
}

func openProtonLocked(raw []byte, password string) ([]authEntry, error) {
	var env struct {
		Version int    `json:"version"`
		Salt    string `json:"salt"`
		Content string `json:"content"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return nil, errors.New("that file is damaged")
	}
	if env.Version != 1 {
		return nil, fmt.Errorf("this Proton backup is version %d; only version 1 is supported", env.Version)
	}
	if password == "" {
		return nil, errNeedPassword
	}
	salt, err1 := base64.StdEncoding.DecodeString(env.Salt)
	sealed, err2 := base64.StdEncoding.DecodeString(env.Content)
	if err1 != nil || err2 != nil || len(salt) != 16 || len(sealed) < 12+16 {
		return nil, errors.New("that file is damaged")
	}
	plain, err := gcmOpen(protonKey(password, salt), []byte("proton.authenticator.export.v1"), sealed)
	if err != nil {
		return nil, errors.New("wrong password")
	}
	es := entriesFromProtonJSON(plain)
	if len(es) == 0 {
		return nil, errors.New("no accounts were found in that file")
	}
	return es, nil
}

// entriesFromProtonJSON reads the JSON inside a Proton backup. Steam entries
// (steam:// links) use a different code scheme and are left out.
func entriesFromProtonJSON(plain []byte) []authEntry {
	var doc struct {
		Entries []struct {
			Content struct {
				URI  string `json:"uri"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"entries"`
	}
	if json.Unmarshal(plain, &doc) != nil {
		return nil
	}
	var out []authEntry
	for _, it := range doc.Entries {
		if e, err := parseOtpauth(it.Content.URI); err == nil {
			out = append(out, e)
		}
	}
	return out
}
