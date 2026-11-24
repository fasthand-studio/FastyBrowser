package main

// fasty-extension://<id>/<path> serves files out of an installed extension's
// directory, so runtime.getURL(), background pages, popups and options pages
// load like they do in other browsers.

/*
#cgo pkg-config: webkitgtk-6.0
#include <stdlib.h>
#include <string.h>
#include <webkit/webkit.h>

static char *sb_ext_root = NULL;

static const char *sb_ext_mime(const char *path) {
	const char *dot = strrchr(path, '.');
	if (dot == NULL) return NULL;
	if (!g_ascii_strcasecmp(dot, ".js") || !g_ascii_strcasecmp(dot, ".mjs")) return "text/javascript";
	if (!g_ascii_strcasecmp(dot, ".css")) return "text/css";
	if (!g_ascii_strcasecmp(dot, ".html") || !g_ascii_strcasecmp(dot, ".htm")) return "text/html";
	if (!g_ascii_strcasecmp(dot, ".json")) return "application/json";
	if (!g_ascii_strcasecmp(dot, ".svg")) return "image/svg+xml";
	if (!g_ascii_strcasecmp(dot, ".wasm")) return "application/wasm";
	if (!g_ascii_strcasecmp(dot, ".woff2")) return "font/woff2";
	if (!g_ascii_strcasecmp(dot, ".woff")) return "font/woff";
	if (!g_ascii_strcasecmp(dot, ".ttf")) return "font/ttf";
	return NULL;
}

static void sb_ext_fail(WebKitURISchemeRequest *req, const char *msg) {
	GError *err = g_error_new_literal(G_IO_ERROR, G_IO_ERROR_NOT_FOUND, msg);
	webkit_uri_scheme_request_finish_error(req, err);
	g_error_free(err);
}

static void sb_ext_request(WebKitURISchemeRequest *req, gpointer data) {
	const char *uri = webkit_uri_scheme_request_get_uri(req);
	const char *sep = strstr(uri, "://");
	if (sb_ext_root == NULL || sep == NULL) { sb_ext_fail(req, "bad uri"); return; }
	char *rest = g_strdup(sep + 3);
	char *cut = strpbrk(rest, "?#");
	if (cut) *cut = 0;
	char *slash = strchr(rest, '/');
	if (slash == NULL) { g_free(rest); sb_ext_fail(req, "no path"); return; }
	*slash = 0;
	char *id = g_uri_unescape_string(rest, NULL);
	char *path = g_uri_unescape_string(slash + 1, NULL);
	g_free(rest);
	if (id == NULL || path == NULL || *id == 0 || *path == 0 || strstr(id, "..") || strstr(path, "..") || strchr(id, '/')) {
		g_free(id); g_free(path); sb_ext_fail(req, "forbidden"); return;
	}
	char *full = g_build_filename(sb_ext_root, id, path, NULL);
	gchar *contents = NULL;
	gsize len = 0;
	if (!g_file_get_contents(full, &contents, &len, NULL)) {
		g_free(id); g_free(path); g_free(full);
		sb_ext_fail(req, "not found");
		return;
	}
	const char *mime = sb_ext_mime(path);
	char *guessed = NULL;
	if (mime == NULL) {
		gboolean uncertain = FALSE;
		char *ct = g_content_type_guess(path, (const guchar *)contents, len, &uncertain);
		guessed = g_content_type_get_mime_type(ct);
		g_free(ct);
		mime = guessed ? guessed : "application/octet-stream";
	}
	GInputStream *stream = g_memory_input_stream_new_from_data(contents, len, g_free);
	WebKitURISchemeResponse *resp = webkit_uri_scheme_response_new(stream, len);
	webkit_uri_scheme_response_set_content_type(resp, mime);
	SoupMessageHeaders *h = soup_message_headers_new(SOUP_MESSAGE_HEADERS_RESPONSE);
	soup_message_headers_append(h, "Access-Control-Allow-Origin", "*");
	soup_message_headers_append(h, "Content-Type", mime);
	soup_message_headers_append(h, "Cache-Control", "no-cache");
	webkit_uri_scheme_response_set_http_headers(resp, h);
	webkit_uri_scheme_request_finish_with_response(req, resp);
	g_object_unref(resp);
	g_object_unref(stream);
	g_free(guessed); g_free(id); g_free(path); g_free(full);
}

static void sb_register_ext_scheme(gpointer ctx, const char *root) {
	g_free(sb_ext_root);
	sb_ext_root = g_strdup(root);
	WebKitWebContext *c = WEBKIT_WEB_CONTEXT(ctx);
	WebKitSecurityManager *sm = webkit_web_context_get_security_manager(c);
	const char *names[] = {"fasty-extension", "chrome-extension", "moz-extension"};
	for (int i = 0; i < 3; i++) {
		webkit_web_context_register_uri_scheme(c, names[i], sb_ext_request, NULL, NULL);
		webkit_security_manager_register_uri_scheme_as_secure(sm, names[i]);
		webkit_security_manager_register_uri_scheme_as_cors_enabled(sm, names[i]);
	}
}
*/
import "C"

import "unsafe"

func registerExtScheme(e *Engine, root string) {
	cs := C.CString(root)
	defer C.free(unsafe.Pointer(cs))
	C.sb_register_ext_scheme(e.ctx, cs)
}
