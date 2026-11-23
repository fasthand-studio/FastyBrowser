package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var sample = []authEntry{
	{Issuer: "GitHub", Account: "me@example.com", Secret: "JBSWY3DPEHPK3PXP"},
	{Issuer: "Odd Name & Co", Account: "a b", Secret: "GEZDGNBVGY3TQOJQ", Algorithm: "SHA256", Digits: 8, Period: 60},
}

func sameAccounts(t *testing.T, got, want []authEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d accounts, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i].normalized(), want[i].normalized()
		if g.Issuer != w.Issuer || g.Account != w.Account || g.Secret != w.Secret ||
			g.Algorithm != w.Algorithm || g.Digits != w.Digits || g.Period != w.Period {
			t.Errorf("account %d: got %+v, want %+v", i, g, w)
		}
	}
}

func TestExportImportPlain(t *testing.T) {
	data := exportPlain(sample)
	if !strings.HasPrefix(string(data), "otpauth://totp/") || strings.Count(string(data), "\n") != 2 {
		t.Fatalf("%q", data)
	}
	got, err := parseImport(data, "")
	if err != nil {
		t.Fatal(err)
	}
	sameAccounts(t, got, sample)
}

func TestExportImportLocked(t *testing.T) {
	if _, err := exportLocked(sample, "abc"); err == nil {
		t.Fatal("a short password must be refused")
	}
	data, err := exportLocked(sample, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "JBSWY3DP") || strings.Contains(string(data), "GitHub") {
		t.Fatal("the locked file shows its contents")
	}
	if _, err := parseImport(data, ""); err != errNeedPassword {
		t.Fatalf("want a password request, got %v", err)
	}
	if _, err := parseImport(data, "wrong one"); err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Fatalf("got %v", err)
	}
	got, err := parseImport(data, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	sameAccounts(t, got, sample)
}

func TestImportOtherApps(t *testing.T) {
	aegis := `{"version":1,"header":{},"db":{"version":2,"entries":[
	  {"type":"totp","name":"me@example.com","issuer":"GitHub","info":{"secret":"JBSWY3DPEHPK3PXP","algo":"SHA1","digits":6,"period":30}},
	  {"type":"hotp","name":"skip","issuer":"X","info":{"secret":"JBSWY3DPEHPK3PXP","counter":1}}]}}`
	got, err := parseImport([]byte(aegis), "")
	if err != nil || len(got) != 1 || got[0].Issuer != "GitHub" || got[0].Account != "me@example.com" {
		t.Fatalf("aegis: %v %+v", err, got)
	}
	if _, err := parseImport([]byte(`{"db":"ENCRYPTEDBLOB","header":{}}`), ""); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("encrypted aegis: %v", err)
	}
	twofas := `{"services":[{"name":"GitHub","secret":"JBSWY3DPEHPK3PXP","otp":{"account":"me","issuer":"GitHub","digits":6,"period":30,"algorithm":"SHA1","tokenType":"TOTP"}}],"schemaVersion":4}`
	got, err = parseImport([]byte(twofas), "")
	if err != nil || len(got) != 1 || got[0].Account != "me" || got[0].Secret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("2fas: %v %+v", err, got)
	}
	proton := `{"version":1,"entries":[{"id":"1","content":{"uri":"otpauth://totp/Proton:me?secret=JBSWY3DPEHPK3PXP&issuer=Proton","entry_type":"Totp","name":"me"},"note":null}]}`
	got, err = parseImport([]byte(proton), "")
	if err != nil || len(got) != 1 || got[0].Issuer != "Proton" {
		t.Fatalf("proton: %v %+v", err, got)
	}
	bitwarden := `{"items":[{"name":"Site","login":{"username":"u","totp":"JBSWY3DPEHPK3PXP"}},{"name":"Other","login":{"totp":"otpauth://totp/Other:x?secret=GEZDGNBVGY3TQOJQ"}}]}`
	got, err = parseImport([]byte(bitwarden), "")
	if err != nil || len(got) != 2 {
		t.Fatalf("bitwarden: %v %+v", err, got)
	}
}

func pbBytes(num int, b []byte) []byte {
	return append([]byte{byte(num<<3 | 2), byte(len(b))}, b...)
}
func pbVarint(num int, v byte) []byte { return []byte{byte(num << 3), v} }

func TestGoogleAuthenticatorMigration(t *testing.T) {
	var one []byte
	one = append(one, pbBytes(1, []byte("12345678901234567890"))...)
	one = append(one, pbBytes(2, []byte("GitHub:alice"))...)
	one = append(one, pbBytes(3, []byte("GitHub"))...)
	one = append(one, pbVarint(4, 1)...)
	one = append(one, pbVarint(5, 1)...)
	one = append(one, pbVarint(6, 2)...)
	hotp := append(pbBytes(1, []byte("abc")), pbVarint(6, 1)...)
	payload := append(pbBytes(1, one), pbBytes(1, hotp)...)
	payload = append(payload, pbVarint(2, 1)...)
	link := "otpauth-migration://offline?data=" + url.QueryEscape(b64std(payload))
	got, err := parseImport([]byte(link), "")
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	if got[0].Issuer != "GitHub" || got[0].Account != "alice" || got[0].Secret != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
		t.Fatalf("%+v", got[0])
	}
	// and it makes the RFC 6238 code
	if c, _ := totpAt(got[0], time.Unix(59, 0)); c != "287082" {
		t.Fatalf("code %s", c)
	}
}

func b64std(b []byte) string {
	const a = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out []byte
	for i := 0; i < len(b); i += 3 {
		var n uint32
		l := 0
		for j := 0; j < 3; j++ {
			n <<= 8
			if i+j < len(b) {
				n |= uint32(b[i+j])
				l++
			}
		}
		out = append(out, a[n>>18&63], a[n>>12&63])
		if l > 1 {
			out = append(out, a[n>>6&63])
		} else {
			out = append(out, '=')
		}
		if l > 2 {
			out = append(out, a[n&63])
		} else {
			out = append(out, '=')
		}
	}
	return string(out)
}

func TestImportRefusesJunk(t *testing.T) {
	for _, bad := range []string{"", "   ", "hello world", `{"nothing":"here"}`, `{not json`} {
		if _, err := parseImport([]byte(bad), ""); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestMergeSkipsDuplicates(t *testing.T) {
	v := newAuthVault(filepath.Join(t.TempDir(), "a.json"), &memKeys{})
	if err := v.Open(); err != nil {
		t.Fatal(err)
	}
	a, s, err := mergeImported(v, sample)
	if err != nil || a != 2 || s != 0 {
		t.Fatalf("%d %d %v", a, s, err)
	}
	a, s, err = mergeImported(v, append([]authEntry{{Issuer: "New", Account: "x", Secret: "MFRGGZDFMZTWQ2LK"}}, sample...))
	if err != nil || a != 1 || s != 2 || len(v.List()) != 3 {
		t.Fatalf("%d %d %v %d", a, s, err, len(v.List()))
	}
}
