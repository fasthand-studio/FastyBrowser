package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B test vectors (8 digits).
func TestTOTPRFC6238Vectors(t *testing.T) {
	const (
		sha1Key   = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"                                                                        // "12345678901234567890"
		sha256Key = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA"                                                    // "12345678901234567890123456789012"
		sha512Key = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNA" // 64 bytes
	)
	cases := []struct {
		algo, key string
		at        int64
		want      string
	}{
		{"SHA1", sha1Key, 59, "94287082"}, {"SHA1", sha1Key, 1111111109, "07081804"},
		{"SHA1", sha1Key, 1111111111, "14050471"}, {"SHA1", sha1Key, 1234567890, "89005924"},
		{"SHA1", sha1Key, 2000000000, "69279037"}, {"SHA1", sha1Key, 20000000000, "65353130"},
		{"SHA256", sha256Key, 59, "46119246"}, {"SHA256", sha256Key, 1111111109, "68084774"},
		{"SHA512", sha512Key, 59, "90693936"}, {"SHA512", sha512Key, 1111111109, "25091201"},
	}
	for _, c := range cases {
		got, err := TOTPCode(c.key, c.algo, 8, 30, time.Unix(c.at, 0))
		if err != nil || got != c.want {
			t.Errorf("%s t=%d: got %q (%v), want %s", c.algo, c.at, got, err, c.want)
		}
	}
}

func TestParseOTPAuth(t *testing.T) {
	a, err := ParseOTPAuth("otpauth://totp/Example:alice@example.com?secret=jbsw y3dp ehpk3pxp&issuer=Example&digits=6&period=30")
	if err != nil {
		t.Fatal(err)
	}
	if a.Issuer != "Example" || a.Name != "alice@example.com" || a.Secret != "JBSWY3DPEHPK3PXP" || a.Algo != "SHA1" || a.Digits != 6 || a.Period != 30 {
		t.Fatalf("parsed wrong: %+v", a)
	}
	if got, _ := TOTPCode(a.Secret, a.Algo, a.Digits, a.Period, time.Unix(59, 0)); got != "996554" {
		t.Errorf("code for JBSWY3DPEHPK3PXP at t=59 = %q, want 996554", got)
	}
	for _, bad := range []string{"https://example.com", "otpauth://hotp/x?secret=ABC", "otpauth://totp/x?secret=!!!", "otpauth://totp/x"} {
		if _, err := ParseOTPAuth(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

type xorProt struct{}

func (xorProt) Protect(b []byte) ([]byte, error) {
	o := make([]byte, len(b))
	for i := range b {
		o[i] = b[i] ^ 0x5a
	}
	return o, nil
}
func (p xorProt) Unprotect(b []byte) ([]byte, error) { return p.Protect(b) }

func TestAuthStoreEncryptsAndDedupes(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadAuthStore(dir, xorProt{})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Add(AuthAccount{Issuer: "GitHub", Name: "me", Secret: "JBSWY3DPEHPK3PXP"})
	if err != nil || a.ID == "" {
		t.Fatalf("add: %v %+v", err, a)
	}
	if _, err := s.Add(AuthAccount{Issuer: "Again", Secret: "jbsw y3dp ehpk3pxp"}); err == nil {
		t.Error("the same secret must not be added twice")
	}
	if _, err := s.Add(AuthAccount{Name: "bad", Secret: "not base32 !!"}); err == nil {
		t.Error("an invalid secret must be rejected")
	}
	s2, err := LoadAuthStore(dir, xorProt{})
	if err != nil || len(s2.Items) != 1 || s2.Items[0].Issuer != "GitHub" {
		t.Fatalf("reload: %v %+v", err, s2.Items)
	}
	v := s2.Views(time.Unix(59, 0))
	if len(v) != 1 || v[0].Code != "996554" || v[0].Remaining != 1 || v[0].Next == v[0].Code {
		t.Errorf("view wrong: %+v", v)
	}
	if strings.Contains(string(mustRead(t, dir)), "JBSWY3DPEHPK3PXP") {
		t.Error("the secret must not be readable in the file")
	}
	if err := s2.Remove(a.ID); err != nil || len(s2.Items) != 0 {
		t.Error("remove")
	}
}

func mustRead(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "authenticator.dat"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
