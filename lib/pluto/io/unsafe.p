package io

import "unsafe"

// unsafeBytes views a string's bytes without copying. The caller must not
// modify them.
func unsafeBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
