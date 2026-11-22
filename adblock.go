package main

// Bridge to Brave's adblock-rust (adblock-ffi). It compiles standard filter
// lists into WebKit's own content-blocking rule format, which is then saved
// through WebKitUserContentFilterStore as before.

/*
#cgo LDFLAGS: ${SRCDIR}/adblock-ffi/target/release/libfasty_adblock.a -ldl -lm -lpthread
#include <stdlib.h>

typedef struct FastyAdblock FastyAdblock;
FastyAdblock* fasty_adblock_new(void);
void fasty_adblock_add_list(FastyAdblock*, const char*, int);
char* fasty_adblock_rules(const FastyAdblock*);
void fasty_adblock_free_str(char*);
void fasty_adblock_free(FastyAdblock*);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// adblockRules turns one or more filter lists into a JSON array of WebKit
// content-blocking rules. format 0 = ABP/uBO, 1 = hosts file.
func adblockRules(lists []string, format int) (string, error) {
	ab := C.fasty_adblock_new()
	if ab == nil {
		return "", fmt.Errorf("adblock: couldn't create engine")
	}
	defer C.fasty_adblock_free(ab)
	for _, text := range lists {
		if text == "" {
			continue
		}
		c := C.CString(text)
		C.fasty_adblock_add_list(ab, c, C.int(format))
		C.free(unsafe.Pointer(c))
	}
	p := C.fasty_adblock_rules(ab)
	if p == nil {
		return "", fmt.Errorf("adblock: couldn't convert filters")
	}
	defer C.fasty_adblock_free_str(p)
	return C.GoString(p), nil
}
