package redact

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := map[string][]string{
		`https://x.com/cb?code=AbC123xyz&state=foo&next=/home`:  {"AbC123xyz", "foo"},
		`{"access_token":"abcDEF12345","name":"bob"}`:           {"abcDEF12345"},
		`Authorization: Bearer abcdefghijklmnop123`:             {"abcdefghijklmnop123"},
		`key eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.sig12345`: {"eyJhbGci"},
		`sk-ant-api03-abcdefghijklmnopqrstu`:                    {"abcdefghijklmnopqrstu"},
		`csrftoken=fJx14AA44BUF5R4GY1yd3p; wd=1372x854`:         {"fJx14AA44BUF5R4GY1yd3p"},
		`{\"password\":\"hunter2\"}`:                            {"hunter2"},
	}
	for in, secrets := range cases {
		out := String(in)
		for _, s := range secrets {
			if strings.Contains(out, s) {
				t.Errorf("%q still has %q: %q", in, s, out)
			}
		}
	}
	if out := String("wd=1372x854 next=/home"); !strings.Contains(out, "1372x854") {
		t.Errorf("over-redacted: %q", out)
	}
	if got := CookieHeader("sessionid=abc; Path=/; SameSite=Lax; Secure"); got != "sessionid=[REDACTED]; Path=/; SameSite=Lax; Secure" {
		t.Errorf("cookie: %q", got)
	}
}
