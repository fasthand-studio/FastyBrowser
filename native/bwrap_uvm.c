// Lets WebKit's web-process sandbox see the NVIDIA UVM device nodes, so that
// GPU video decoding (NVDEC through VA-API) can start inside it.
//
// WebKitGTK starts each web process through GLib's
// g_spawn_async_with_pipes_and_fds with
//     /usr/bin/bwrap --args FD -- /usr/lib/webkitgtk-6.0/WebKitWebProcess ...
// where FD is a descriptor holding the sandbox's arguments (NUL-separated).
// WebKit binds /dev/nvidia0 and /dev/nvidiactl into the sandbox but not
// /dev/nvidia-uvm, which CUDA and NVDEC need. This library is preloaded into
// Fasty only when GPU decoding is turned on. It watches for that bwrap start
// and adds
//     --dev-bind-try /dev/nvidia-uvm /dev/nvidia-uvm   (and -uvm-tools)
// right after the sandbox's "--dev /dev". Nothing else changes, and nothing is
// added if the device nodes don't exist.

#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>

static const char *EXTRA[] = {"/dev/nvidia-uvm", "/dev/nvidia-uvm-tools", NULL};
static int debug;

__attribute__((constructor)) static void init(void) {
	debug = getenv("FASTY_HOOK_DEBUG") != NULL;
}

// envp without LD_PRELOAD: the processes Fasty starts must not preload this
// (unsetting it here instead stops it loading in Fasty's own re-exec).
static const char *const *clean_env(const char *const *envp, const char ***owned) {
	*owned = NULL;
	if (!envp) return envp;
	size_t n = 0;
	int has = 0;
	for (; envp[n]; n++)
		if (strncmp(envp[n], "LD_PRELOAD=", 11) == 0) has = 1;
	if (!has) return envp;
	const char **out = malloc((n + 1) * sizeof(char *));
	if (!out) return envp;
	size_t k = 0;
	for (size_t i = 0; i < n; i++)
		if (strncmp(envp[i], "LD_PRELOAD=", 11) != 0) out[k++] = envp[i];
	out[k] = NULL;
	*owned = out;
	return out;
}

static int is_bwrap(const char *path) {
	size_t n = path ? strlen(path) : 0;
	return n >= 5 && strcmp(path + n - 5, "bwrap") == 0;
}

// Read everything in fd (from the start when it can seek).
static char *slurp(int fd, size_t *out) {
	if (lseek(fd, 0, SEEK_SET) < 0 && errno != ESPIPE) return NULL;
	size_t cap = 1 << 16, len = 0;
	char *buf = malloc(cap);
	if (!buf) return NULL;
	for (;;) {
		if (len == cap) {
			cap *= 2;
			char *nb = realloc(buf, cap);
			if (!nb) { free(buf); return NULL; }
			buf = nb;
		}
		ssize_t r = read(fd, buf + len, cap - len);
		if (r < 0 && errno == EINTR) continue;
		if (r <= 0) break;
		len += (size_t)r;
	}
	*out = len;
	return buf;
}

// A new descriptor with the sandbox arguments plus the UVM binds, or -1.
static int rewritten_args(int fd) {
	size_t len = 0;
	char *buf = slurp(fd, &len);
	if (!buf) return -1;

	// find the "--dev" "/dev" pair
	size_t at = 0, pos = 0;
	int found = 0;
	while (pos < len) {
		const char *tok = buf + pos;
		size_t tl = strnlen(tok, len - pos);
		if (strcmp(tok, "--dev") == 0 && pos + tl + 1 < len && strcmp(buf + pos + tl + 1, "/dev") == 0) {
			at = pos + tl + 1 + strlen("/dev") + 1;
			found = 1;
			break;
		}
		pos += tl + 1;
	}
	int mfd = memfd_create("fasty-bwrap-args", 0);
	if (mfd < 0) { free(buf); return -1; }
	size_t head = found ? at : len;
	(void)!write(mfd, buf, head);
	if (found) {
		for (int i = 0; EXTRA[i]; i++) {
			struct stat st;
			if (stat(EXTRA[i], &st) != 0) continue;
			(void)!write(mfd, "--dev-bind-try", sizeof "--dev-bind-try");
			(void)!write(mfd, EXTRA[i], strlen(EXTRA[i]) + 1);
			(void)!write(mfd, EXTRA[i], strlen(EXTRA[i]) + 1);
		}
		(void)!write(mfd, buf + at, len - at);
	}
	lseek(mfd, 0, SEEK_SET);
	free(buf);
	if (debug) fprintf(stderr, "fasty: uvm hook: bwrap arguments rewritten (%s)\n", found ? "added" : "no --dev found");
	return mfd;
}

typedef int (*spawn_fn)(const char *, const char *const *, const char *const *, int, void *, void *, int, int, int,
			const int *, const int *, size_t, int *, int *, int *, int *, void **);

int g_spawn_async_with_pipes_and_fds(const char *wd, const char *const *argv, const char *const *envp, int flags,
				     void *setup, void *ud, int in, int out, int err, const int *src, const int *dst,
				     size_t n, int *pid, int *pin, int *pout, int *perr, void **error) {
	static spawn_fn real;
	if (!real) real = (spawn_fn)dlsym(RTLD_NEXT, "g_spawn_async_with_pipes_and_fds");

	const char **ownedenv;
	envp = clean_env(envp, &ownedenv);
	int newfd = -1;
	int *src2 = NULL;
	if (argv && argv[0] && is_bwrap(argv[0])) {
		int target = -1;
		for (int i = 0; argv[i]; i++)
			if (strcmp(argv[i], "--args") == 0 && argv[i + 1]) { target = atoi(argv[i + 1]); break; }
		int web = 0;
		for (int i = 0; argv[i]; i++)
			if (strstr(argv[i], "WebKitWebProcess")) web = 1;
		if (!web) target = -1; // only the web process needs it, not xdg-dbus-proxy
		int mapped = 0;
		// The descriptor may be handed over under another number...
		for (size_t k = 0; target >= 0 && src && dst && k < n; k++) {
			if (dst[k] != target) continue;
			mapped = 1;
			if ((newfd = rewritten_args(src[k])) >= 0) {
				src2 = malloc(n * sizeof(int));
				if (src2) {
					memcpy(src2, src, n * sizeof(int));
					src2[k] = newfd;
					src = src2;
				} else {
					close(newfd);
					newfd = -1;
				}
			}
			break;
		}
		// ...or inherited under its own number: put the new contents there.
		if (!mapped && target >= 0 && fcntl(target, F_GETFD) != -1) {
			int mfd = rewritten_args(target);
			if (mfd >= 0) {
				dup2(mfd, target);
				close(mfd);
			}
		}
	}
	int ret = real(wd, argv, envp, flags, setup, ud, in, out, err, src, dst, n, pid, pin, pout, perr, error);
	if (newfd >= 0) close(newfd);
	free(src2);
	free(ownedenv);
	return ret;
}
