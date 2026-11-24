package main

// Reading HTTP headers through the Go bindings wraps a NULL SoupMessageHeaders
// (which WebKit returns for requests it hasn't built headers for yet) in an
// object whose finalizer then aborts the process. These helpers read the
// headers in C and hand back plain text, "name\x01value\n" per header.

/*
#cgo pkg-config: webkitgtk-6.0
#include <stdlib.h>
#include <webkit/webkit.h>

static void sb_hdr_add(const char *k, const char *v, gpointer data) {
	GString *s = data;
	g_string_append(s, k);
	g_string_append_c(s, '\x01');
	g_string_append(s, v);
	g_string_append_c(s, '\n');
}

static char *sb_pack(SoupMessageHeaders *h) {
	if (h == NULL) return NULL;
	GString *s = g_string_new(NULL);
	soup_message_headers_foreach(h, sb_hdr_add, s);
	return g_string_free(s, FALSE);
}

static char *sb_request_headers(gpointer r) {
	if (r == NULL || !WEBKIT_IS_URI_REQUEST(r)) return NULL;
	return sb_pack(webkit_uri_request_get_http_headers(WEBKIT_URI_REQUEST(r)));
}

static char *sb_response_headers(gpointer r) {
	if (r == NULL || !WEBKIT_IS_URI_RESPONSE(r)) return NULL;
	return sb_pack(webkit_uri_response_get_http_headers(WEBKIT_URI_RESPONSE(r)));
}

static int sb_is_response(gpointer r) { return r != NULL && WEBKIT_IS_URI_RESPONSE(r); }
static unsigned sb_status(gpointer r) { return sb_is_response(r) ? webkit_uri_response_get_status_code(WEBKIT_URI_RESPONSE(r)) : 0; }
static const char *sb_mime(gpointer r) { return sb_is_response(r) ? webkit_uri_response_get_mime_type(WEBKIT_URI_RESPONSE(r)) : NULL; }

*/
import "C"

import (
	"fastybrowser/src/redact"
	"strings"
	"unsafe"

	webkit "github.com/diamondburned/gotk4-webkitgtk/pkg/webkit/v6"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

func unpackHeaders(c *C.char) [][]string {
	if c == nil {
		return nil
	}
	defer C.g_free(C.gpointer(unsafe.Pointer(c)))
	var out [][]string
	for _, line := range strings.Split(C.GoString(c), "\n") {
		if k, v, ok := strings.Cut(line, "\x01"); ok {
			out = append(out, []string{k, redact.Header(k, v)})
		}
	}
	return out
}

// ptr is the C pointer behind a binding object, or nil when the object is a
// wrapper around NULL.
func ptr(o coreglib.Objector) (p C.gpointer) {
	defer func() {
		if recover() != nil {
			p = nil
		}
	}()
	if o == nil {
		return nil
	}
	return native(coreglib.BaseObject(o))
}

func requestHeaders(r *webkit.URIRequest) [][]string {
	if r == nil {
		return nil
	}
	return unpackHeaders(C.sb_request_headers(ptr(r)))
}

func responseHeaders(r *webkit.URIResponse) [][]string {
	if r == nil {
		return nil
	}
	return unpackHeaders(C.sb_response_headers(ptr(r)))
}

func responseStatus(r *webkit.URIResponse) int {
	if r == nil {
		return 0
	}
	return int(C.sb_status(ptr(r)))
}

func responseMime(r *webkit.URIResponse) string {
	if r == nil {
		return ""
	}
	return C.GoString(C.sb_mime(ptr(r)))
}
