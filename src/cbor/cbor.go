// The little CBOR a passkey's attestation needs.
// Package cbor is the little CBOR encoder a passkey attestation needs.
package cbor

import (
	"encoding/binary"
)

// ---- CBOR (just what a passkey needs) -----------------------------------------

func Head(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	case n < 65536:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	default:
		b := []byte{major<<5 | 26, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], uint32(n))
		return b
	}
}

func Int(v int) []byte {
	if v >= 0 {
		return Head(0, uint64(v))
	}
	return Head(1, uint64(-1-v))
}

func Bytes(b []byte) []byte { return append(Head(2, uint64(len(b))), b...) }

func Text(s string) []byte { return append(Head(3, uint64(len(s))), s...) }

func Map(pairs ...[]byte) []byte {
	out := Head(5, uint64(len(pairs)/2))
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}
