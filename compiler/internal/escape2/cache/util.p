// Translated by go2pu from internal/escape2/cache/util.go at the go-cutover tag; edited by hand since.

package cache

import (
	"pluto/slices"
	"compiler/compat"
)

func sortStrings(s []string) { slices.Sort(s) }

// hexDigest is d in hexadecimal. A digest's bytes are not UTF-8, so a
// string of them (compat.String) would turn most into U+FFFD.
func hexDigest(d digest) string {
	const hexDigits = "0123456789abcdef"
	b := compat.MakeCap[byte](0, 2*len(d))
	for _, x := range d {
		b = compat.AppendPlain(b, hexDigits[x>>4], hexDigits[x&15])
	}
	return compat.String(b)
}
