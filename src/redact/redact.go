// Package redact removes secrets from text, headers, cookies and JSON values.
package redact

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// The agent socket hands page text, cookies and recorded network traffic to
// scripts and AI agents. Secrets in that must never reach them, so every reply
// is passed through redactReply before it is written. Values are replaced by
// [REDACTED]; names, headers and cookie attributes stay, so an agent can still
// see what was sent and how a cookie was set.

const Placeholder = "[REDACTED]"

// sensitiveNames are the parameter, field, header and cookie names whose
// values are secrets (matched whole, case-insensitively, after splitting off
// common prefixes like "x-" and "access_").
const sensitiveNames = `(?:[a-z0-9_.\-]*(?:token|secret|passw(?:or)?d|passwd|pwd|apikey|api[_-]?key|private[_-]?key|credential|encrypted[a-z_]*|signature|authorization|auth|csrf|xsrf|sessionid|session[_-]?id|session|cookie|otp|2fa|jazoest|fb_dtsg|dtsg|lsd|nonce)[a-z0-9_.\-]*|key|code|sig|sid|ssid|hsid|sapisid|apisid|sidcc|xs|c_user|ds_user_id|rur|sb|datr|fr|mid|ig_did|ig_nrcb|pin|state|__[a-z]*_?(?:sid|token))`

var (
	reJWT    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`)
	reBearer = regexp.MustCompile(`(?i)\b(bearer|basic|token)(\s+)[A-Za-z0-9._~+/=\-]{12,}`)
	reKnown  = regexp.MustCompile(`\b(?:sk-(?:ant-)?[A-Za-z0-9_\-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9\-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_\-]{30,}|EAA[A-Za-z0-9]{40,}|ya29\.[A-Za-z0-9_\-]{20,}|glpat-[A-Za-z0-9_\-]{16,}|npm_[A-Za-z0-9]{30,}|shp(?:at|ss|ca)_[a-f0-9]{20,}|whsec_[A-Za-z0-9]{16,}|[rsp]k_(?:live|test)_[A-Za-z0-9]{16,}|-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----)`)
	// "name":"value" in JSON, also when that JSON sits inside a string (\")
	reJSON = regexp.MustCompile(`(?i)(\\?"` + sensitiveNames + `\\?"\s*:\s*\\?")((?:[^"\\]|\\.)*?)(\\?")`)
	// name=value in URLs, forms and cookie headers
	rePair = regexp.MustCompile(`(?i)((?:^|[?&;,\s"'])` + sensitiveNames + `=)([^&;\s"'<>,]+)`)
	// header lines: "Authorization: ..." (the whole value)
	reHeader = regexp.MustCompile(`(?im)^(\s*(?:authorization|proxy-authorization|x-[a-z\-]*(?:token|key|secret|csrf|auth)[a-z\-]*)\s*:\s*).+$`)
)

// redactedLen is the placeholder for a secret. It keeps the length (an empty
// or "undefined" token looks different from a real one) and nothing else.
func redactedLen(v string) string {
	if v == "" {
		return "[EMPTY]"
	}
	return "[REDACTED " + strconv.Itoa(len(v)) + " chars]"
}

// String removes secrets from free text.
func String(s string) string {
	if len(s) < 6 {
		return s
	}
	s = reKnown.ReplaceAllString(s, Placeholder)
	s = reJWT.ReplaceAllString(s, Placeholder)
	s = reBearer.ReplaceAllString(s, "${1}${2}"+Placeholder)
	s = reHeader.ReplaceAllString(s, "${1}"+Placeholder)
	s = reJSON.ReplaceAllStringFunc(s, func(m string) string {
		p := reJSON.FindStringSubmatch(m)
		if strings.HasPrefix(p[2], "[REDACTED") || p[2] == "[EMPTY]" {
			return m
		}
		return p[1] + redactedLen(p[2]) + p[3]
	})
	s = rePair.ReplaceAllStringFunc(s, func(m string) string {
		p := rePair.FindStringSubmatch(m)
		if strings.HasPrefix(p[2], "[REDACTED") {
			return m
		}
		return p[1] + redactedLen(p[2])
	})
	return s
}

// CookieHeader hides the values of "a=b; c=d" (Cookie) or
// "a=b; Path=/; SameSite=Lax" (Set-Cookie): names and attributes stay.
func CookieHeader(v string) string {
	parts := strings.Split(v, ";")
	for i, p := range parts {
		name, _, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		n := strings.ToLower(strings.TrimSpace(name))
		switch n {
		case "path", "domain", "expires", "max-age", "samesite", "secure", "httponly", "priority", "partitioned":
			continue
		}
		parts[i] = name + "=" + Placeholder
	}
	return strings.Join(parts, ";")
}

// Header hides a header value by its name.
func Header(name, value string) string {
	switch n := strings.ToLower(name); {
	case n == "cookie" || n == "set-cookie":
		return CookieHeader(value)
	case n == "authorization" || n == "proxy-authorization":
		return Placeholder
	case strings.Contains(n, "token") || strings.Contains(n, "lsd") || strings.Contains(n, "nonce") || strings.Contains(n, "dtsg") || strings.Contains(n, "session") || strings.Contains(n, "secret") || strings.Contains(n, "csrf") ||
		strings.Contains(n, "api-key") || strings.Contains(n, "apikey") || strings.Contains(n, "auth"):
		return Placeholder
	case n == "location" || n == "referer" || n == "origin" || n == "content-location":
		return String(value)
	}
	return String(value)
}

func Value(v any) any {
	switch x := v.(type) {
	case string:
		return String(x)
	case []any:
		for i := range x {
			x[i] = Value(x[i])
		}
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && regexp.MustCompile(`(?i)^`+sensitiveNames+`$`).MatchString(k) && len(k) > 3 {
				if len(s) > 0 {
					x[k] = Placeholder
				}
				continue
			}
			x[k] = Value(e)
		}
	}
	return v
}

func MarshalNoEscape(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return b.Bytes()
}
