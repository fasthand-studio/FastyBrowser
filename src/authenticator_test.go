package main

import (
	"path/filepath"
	"testing"
	"time"
)

// RFC 6238, appendix B: the 8-digit test codes.
func TestTOTPVectors(t *testing.T) {
	secrets := map[string]string{
		"SHA1":   "12345678901234567890",
		"SHA256": "12345678901234567890123456789012",
		"SHA512": "1234567890123456789012345678901234567890123456789012345678901234",
	}
	b32 := func(s string) string {
		const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
		var out []byte
		var bits, n uint
		for _, c := range []byte(s) {
			bits = bits<<8 | uint(c)
			n += 8
			for n >= 5 {
				out = append(out, alpha[(bits>>(n-5))&31])
				n -= 5
			}
		}
		if n > 0 {
			out = append(out, alpha[(bits<<(5-n))&31])
		}
		return string(out)
	}
	cases := []struct {
		at   int64
		want map[string]string
	}{
		{59, map[string]string{"SHA1": "94287082", "SHA256": "46119246", "SHA512": "90693936"}},
		{1111111109, map[string]string{"SHA1": "07081804", "SHA256": "68084774", "SHA512": "25091201"}},
		{1111111111, map[string]string{"SHA1": "14050471", "SHA256": "67062674", "SHA512": "99943326"}},
		{1234567890, map[string]string{"SHA1": "89005924", "SHA256": "91819424", "SHA512": "93441116"}},
		{2000000000, map[string]string{"SHA1": "69279037", "SHA256": "90698825", "SHA512": "38618901"}},
		{20000000000, map[string]string{"SHA1": "65353130", "SHA256": "77737706", "SHA512": "47863826"}},
	}
	for _, c := range cases {
		for alg, want := range c.want {
			e := authEntry{Secret: b32(secrets[alg]), Algorithm: alg, Digits: 8, Period: 30}
			got, err := totpAt(e, time.Unix(c.at, 0))
			if err != nil || got != want {
				t.Errorf("%s at %d: got %q (%v), want %q", alg, c.at, got, err, want)
			}
		}
	}
}

func TestSecretFormats(t *testing.T) {
	a, err1 := decodeSecret("GEZD GNBV-GY3T QOJQ")
	b, err2 := decodeSecret("gezdgnbvgy3tqojq====")
	if err1 != nil || err2 != nil || string(a) != string(b) || string(a) != "1234567890" {
		t.Fatalf("got %q %q %v %v", a, b, err1, err2)
	}
	if _, err := decodeSecret("not base32 !!"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseOtpauth(t *testing.T) {
	e, err := parseOtpauth("otpauth://totp/Example:alice@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example&digits=6&period=30&algorithm=SHA1")
	if err != nil || e.Issuer != "Example" || e.Account != "alice@example.com" || e.Digits != 6 || e.Period != 30 {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = parseOtpauth("otpauth://totp/Bob?secret=JBSWY3DPEHPK3PXP")
	if err != nil || e.Account != "Bob" || e.Issuer != "" || e.Algorithm != "SHA1" {
		t.Fatalf("%+v %v", e, err)
	}
	for _, bad := range []string{"https://example.com", "otpauth://hotp/x?secret=JBSWY3DPEHPK3PXP", "otpauth://totp/x?secret=!!!"} {
		if _, err := parseOtpauth(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestGroupAndTimer(t *testing.T) {
	if groupCode("209117") != "209 117" || groupCode("12345678") != "1234 5678" {
		t.Fatal("groupCode")
	}
	e := authEntry{Period: 30}
	if e.secondsLeft(time.Unix(60, 0)) != 30 || e.secondsLeft(time.Unix(89, 0)) != 1 {
		t.Fatal("secondsLeft")
	}
}

type memKeys struct{ key []byte }

func (m *memKeys) load() ([]byte, error) {
	if m.key == nil {
		return nil, errLocked
	}
	return m.key, nil
}
func (m *memKeys) save(k []byte) error { m.key = append([]byte(nil), k...); return nil }
func (m *memKeys) remove() error       { m.key = nil; return nil }

func TestVaultAndPIN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	keys := &memKeys{}
	v := newAuthVault(path, keys)
	if err := v.Open(); err != nil {
		t.Fatal(err)
	}
	if v.hasPIN() {
		t.Fatal("the PIN must be off by default")
	}
	if err := v.Save(authEntry{Issuer: "GitHub", Account: "me", Secret: "JBSWY3DPEHPK3PXP"}); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(authEntry{Issuer: "Bad", Secret: "!!!"}); err == nil {
		t.Fatal("a bad secret must be refused")
	}
	// reopen from disk
	v2 := newAuthVault(path, keys)
	if err := v2.Open(); err != nil || len(v2.List()) != 1 || v2.List()[0].Issuer != "GitHub" {
		t.Fatalf("%v %+v", err, v2.List())
	}
	// turn the PIN on: the key leaves the key store
	if err := v2.SetPIN("123"); err == nil {
		t.Fatal("a short PIN must be refused")
	}
	if err := v2.SetPIN("2468"); err != nil {
		t.Fatal(err)
	}
	if keys.key != nil {
		t.Fatal("the key should be gone from the key store once there is a PIN")
	}
	v2.Lock()
	v3 := newAuthVault(path, keys)
	if err := v3.Open(); err != errLocked {
		t.Fatalf("a vault with a PIN must stay locked: %v", err)
	}
	if err := v3.Unlock("0000"); err == nil || v3.unlocked() {
		t.Fatal("a wrong PIN must not open it")
	}
	if err := v3.Unlock("2468"); err != nil || len(v3.List()) != 1 {
		t.Fatalf("%v %+v", err, v3.List())
	}
	// the wait after wrong tries
	v4 := newAuthVault(path, keys)
	for i := 0; i < 3; i++ {
		v4.Unlock("9999")
	}
	if v4.WaitFor() <= 0 {
		t.Fatal("three wrong PINs should cost a wait")
	}
	if err := v4.Unlock("2468"); err == nil {
		t.Fatal("even the right PIN has to wait")
	}
	// turn it off again
	if err := v3.RemovePIN("1111"); err == nil || !v3.hasPIN() {
		t.Fatal("a wrong PIN must not turn it off")
	}
	if err := v3.RemovePIN("2468"); err != nil || v3.hasPIN() || keys.key == nil {
		t.Fatalf("%v", err)
	}
	v5 := newAuthVault(path, keys)
	if err := v5.Open(); err != nil || len(v5.List()) != 1 {
		t.Fatalf("%v", err)
	}
	if err := v5.Delete(v5.List()[0].ID); err != nil || len(v5.List()) != 0 {
		t.Fatal("delete")
	}
}
