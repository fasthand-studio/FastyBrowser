package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AuthAccount is one account in the Authenticator: a shared secret that makes time-based codes (RFC 6238).
type AuthAccount struct {
	ID     string `json:"id"`
	Issuer string `json:"issuer"`
	Name   string `json:"name"`
	Secret string `json:"secret"` // base32
	Algo   string `json:"algo"`   // SHA1 | SHA256 | SHA512
	Digits int    `json:"digits"` // 6 or 8
	Period int    `json:"period"` // seconds
}

func hashFor(algo string) (func() hash.Hash, error) {
	switch strings.ToUpper(algo) {
	case "", "SHA1":
		return sha1.New, nil
	case "SHA256":
		return sha256.New, nil
	case "SHA512":
		return sha512.New, nil
	}
	return nil, errors.New("unsupported algorithm " + algo)
}

func decodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "").Replace(s))
	if s == "" {
		return nil, errors.New("empty secret")
	}
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return nil, errors.New("the secret key is not valid base32")
	}
	return b, nil
}

// TOTPCode returns the code for a time. digits is 6 to 8, period is in seconds.
func TOTPCode(secret, algo string, digits, period int, at time.Time) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	h, err := hashFor(algo)
	if err != nil {
		return "", err
	}
	if digits < 6 || digits > 8 {
		return "", errors.New("digits must be 6, 7 or 8")
	}
	if period <= 0 {
		return "", errors.New("period must be positive")
	}
	var ctr [8]byte
	binary.BigEndian.PutUint64(ctr[:], uint64(at.Unix()/int64(period)))
	m := hmac.New(h, key)
	m.Write(ctr[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod), nil
}

// ParseOTPAuth reads an otpauth://totp/... link (what a QR code on a setup page holds).
func ParseOTPAuth(raw string) (AuthAccount, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "otpauth" {
		return AuthAccount{}, errors.New("not an otpauth:// link")
	}
	if u.Host != "totp" {
		return AuthAccount{}, errors.New("only time-based (totp) accounts are supported")
	}
	q := u.Query()
	label := strings.TrimPrefix(u.Path, "/")
	if l, err := url.PathUnescape(label); err == nil {
		label = l
	}
	a := AuthAccount{Secret: q.Get("secret"), Algo: strings.ToUpper(q.Get("algorithm")), Issuer: q.Get("issuer")}
	if i := strings.Index(label, ":"); i >= 0 {
		if a.Issuer == "" {
			a.Issuer = strings.TrimSpace(label[:i])
		}
		label = label[i+1:]
	}
	a.Name = strings.TrimSpace(label)
	a.Digits, _ = strconv.Atoi(q.Get("digits"))
	a.Period, _ = strconv.Atoi(q.Get("period"))
	return a.normalized()
}

func (a AuthAccount) normalized() (AuthAccount, error) {
	if a.Algo == "" {
		a.Algo = "SHA1"
	}
	if a.Digits == 0 {
		a.Digits = 6
	}
	if a.Period == 0 {
		a.Period = 30
	}
	a.Secret = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(a.Secret))
	if _, err := TOTPCode(a.Secret, a.Algo, a.Digits, a.Period, time.Unix(0, 0)); err != nil {
		return a, err
	}
	if a.Issuer == "" && a.Name == "" {
		a.Name = "Account"
	}
	return a, nil
}

// Protector encrypts the account file. On Windows it is DPAPI, tied to the signed-in user.
type Protector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
}

type AuthStore struct {
	dir   string
	prot  Protector
	Items []AuthAccount
}

func LoadAuthStore(dir string, p Protector) (*AuthStore, error) {
	s := &AuthStore{dir: dir, prot: p}
	data, err := os.ReadFile(filepath.Join(dir, "authenticator.dat"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	plain, err := p.Unprotect(data)
	if err != nil {
		return s, errors.New("the saved accounts could not be decrypted (they belong to another Windows user)")
	}
	return s, json.Unmarshal(plain, &s.Items)
}

func (s *AuthStore) save() error {
	plain, err := json.Marshal(s.Items)
	if err != nil {
		return err
	}
	enc, err := s.prot.Protect(plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.dir, "authenticator.dat"), enc)
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Add stores a new account; the same secret is never added twice.
func (s *AuthStore) Add(a AuthAccount) (AuthAccount, error) {
	a, err := a.normalized()
	if err != nil {
		return a, err
	}
	for _, it := range s.Items {
		if it.Secret == a.Secret {
			return it, errors.New("this account is already added")
		}
	}
	a.ID = newID()
	s.Items = append(s.Items, a)
	return a, s.save()
}

func (s *AuthStore) Remove(id string) error {
	for i, it := range s.Items {
		if it.ID == id {
			s.Items = append(s.Items[:i], s.Items[i+1:]...)
			return s.save()
		}
	}
	return nil
}

// AuthView is what the Authenticator page shows: codes only, never the secrets.
type AuthView struct {
	ID        string `json:"id"`
	Issuer    string `json:"issuer"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Next      string `json:"next"`
	Remaining int    `json:"remaining"`
	Period    int    `json:"period"`
}

func (s *AuthStore) Views(at time.Time) []AuthView {
	out := make([]AuthView, 0, len(s.Items))
	for _, it := range s.Items {
		code, _ := TOTPCode(it.Secret, it.Algo, it.Digits, it.Period, at)
		next, _ := TOTPCode(it.Secret, it.Algo, it.Digits, it.Period, at.Add(time.Duration(it.Period)*time.Second))
		rem := it.Period - int(at.Unix()%int64(it.Period))
		out = append(out, AuthView{ID: it.ID, Issuer: it.Issuer, Name: it.Name, Code: code, Next: next, Remaining: rem, Period: it.Period})
	}
	return out
}
