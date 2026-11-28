//go:build windows

package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32              = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtectData = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect   = crypt32.NewProc("CryptUnprotectData")
	procLocalFree        = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cb uint32
	pb *byte
}

func blobOf(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{uint32(len(b)), &b[0]}
}

var dpapiEntropy = []byte("FastHand Fasty Authenticator")

// dpapi encrypts with the signed-in Windows user's key (DPAPI): the file is useless on another
// account or another PC, and Fasty keeps no key of its own.
type dpapi struct{}

func (dpapi) run(proc *windows.LazyProc, in []byte) ([]byte, error) {
	src := blobOf(in)
	ent := blobOf(dpapiEntropy)
	var out dataBlob
	r, _, err := proc.Call(uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&ent)), 0, 0, 0, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errors.New("Windows data protection failed: " + err.Error())
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pb)))
	res := make([]byte, out.cb)
	copy(res, unsafe.Slice(out.pb, out.cb))
	return res, nil
}

func (d dpapi) Protect(b []byte) ([]byte, error)   { return d.run(procCryptProtectData, b) }
func (d dpapi) Unprotect(b []byte) ([]byte, error) { return d.run(procCryptUnprotect, b) }
