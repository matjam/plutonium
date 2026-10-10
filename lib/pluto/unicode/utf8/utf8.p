// Package utf8 implements functions and constants to support text encoded
// in UTF-8. It mirrors Go's unicode/utf8. Nothing here allocates, except
// AppendRune into a growing vec.Vec: the byte slice functions view their
// argument as a string without copying.
//
// Adapted from Go's unicode/utf8 package.
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package utf8

import (
	"pluto/rt"
	"pluto/vec"
	"unsafe"
)

// Numbers fundamental to the encoding.
const (
	RuneError = '\uFFFD'     // the "error" Rune or "Unicode replacement character"
	RuneSelf  = 0x80         // characters below RuneSelf are represented as themselves in a single byte
	MaxRune   = '\U0010FFFF' // maximum valid Unicode code point
	UTFMax    = 4            // maximum number of bytes of a UTF-8 encoded Unicode character
)

// Code points in the surrogate range are not valid for UTF-8.
const (
	surrogateMin = 0xD800
	surrogateMax = 0xDFFF
)

const (
	tx = 0b10000000
	t2 = 0b11000000
	t3 = 0b11100000
	t4 = 0b11110000

	maskx = 0b00111111
	mask2 = 0b00011111
	mask3 = 0b00001111
	mask4 = 0b00000111

	rune1Max = 1<<7 - 1
	rune2Max = 1<<11 - 1
	rune3Max = 1<<16 - 1

	// The default lowest and highest continuation byte.
	locb = 0b10000000
	hicb = 0b10111111

	// These names of these constants are chosen to give nice alignment in the
	// table below. The first nibble is an index into acceptRanges or F for
	// special one-byte cases. The second nibble is the Rune length or the
	// Status for the special one-byte case.
	xx = 0xF1 // invalid: size 1
	as = 0xF0 // ASCII: size 1
	s1 = 0x02 // accept 0, size 2
	s2 = 0x13 // accept 1, size 3
	s3 = 0x03 // accept 0, size 3
	s4 = 0x23 // accept 2, size 3
	s5 = 0x34 // accept 3, size 4
	s6 = 0x04 // accept 0, size 4
	s7 = 0x44 // accept 4, size 4
)

const (
	runeErrorByte0 = t3 | (RuneError >> 12)
	runeErrorByte1 = tx | (RuneError>>6)&maskx
	runeErrorByte2 = tx | RuneError&maskx
)

// first is information about the first byte in a UTF-8 sequence.
var first = [256]uint8{
	//   1   2   3   4   5   6   7   8   9   A   B   C   D   E   F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x00-0x0F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x10-0x1F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x20-0x2F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x30-0x3F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x40-0x4F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x50-0x5F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x60-0x6F
	as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, as, // 0x70-0x7F
	//   1   2   3   4   5   6   7   8   9   A   B   C   D   E   F
	xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, // 0x80-0x8F
	xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, // 0x90-0x9F
	xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, // 0xA0-0xAF
	xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, // 0xB0-0xBF
	xx, xx, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, // 0xC0-0xCF
	s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, s1, // 0xD0-0xDF
	s2, s3, s3, s3, s3, s3, s3, s3, s3, s3, s3, s3, s3, s4, s3, s3, // 0xE0-0xEF
	s5, s6, s6, s6, s7, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, xx, // 0xF0-0xFF
}

// acceptRange gives the range of valid values for the second byte in a UTF-8
// sequence.
type acceptRange struct {
	lo uint8 // lowest value for second byte
	hi uint8 // highest value for second byte
}

// acceptRanges has size 16 to avoid bounds checks in the code that uses it.
var acceptRanges = [16]acceptRange{
	0: {locb, hicb},
	1: {0xA0, hicb},
	2: {locb, 0x9F},
	3: {0x90, hicb},
	4: {locb, 0x8F},
}

// str views p as a string without copying. The bytes need not be valid
// UTF-8: it is the point of Valid to find that out, so the view is read by
// ValidString and the functions here that decode a rune, which treat
// invalid bytes as errors, and goes no further.
func str(p []byte) string {
	if len(p) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(p), len(p))
}

// FullRune reports whether the bytes in p begin with a full UTF-8 encoding
// of a rune. An invalid encoding is considered a full Rune since it will
// convert as a width-1 error rune.
func FullRune(p []byte) bool { return FullRuneInString(str(p)) }

// FullRuneInString is like FullRune but its input is a string.
func FullRuneInString(s string) bool {
	n := len(s)
	if n == 0 {
		return false
	}
	x := first[s[0]]
	if n >= int(x&7) {
		return true // ASCII, invalid, or valid
	}
	// Must be short or invalid.
	accept := acceptRanges[x>>4]
	if n > 1 && (s[1] < accept.lo || accept.hi < s[1]) {
		return true
	} else if n > 2 && (s[2] < locb || hicb < s[2]) {
		return true
	}
	return false
}

// DecodeRune unpacks the first UTF-8 encoding in p and returns the rune and
// its width in bytes. If p is empty it returns (RuneError, 0). Otherwise, if
// the encoding is invalid, it returns (RuneError, 1). Both are impossible
// results for correct, non-empty UTF-8.
//
// An encoding is invalid if it is incorrect UTF-8, encodes a rune that is
// out of range, or is not the shortest possible UTF-8 encoding for the
// value. No other validation is performed.
func DecodeRune(p []byte) (r rune, size int) { return DecodeRuneInString(str(p)) }

// DecodeRuneInString is like DecodeRune but its input is a string.
func DecodeRuneInString(s string) (rune, int) {
	n := len(s)
	if n < 1 {
		return RuneError, 0
	}
	s0 := s[0]
	if s0 < RuneSelf {
		return rune(s0), 1
	}
	x := first[s0]
	if x >= as {
		// Not a valid first byte (ASCII was handled above).
		return RuneError, 1
	}
	sz := int(x & 7)
	accept := acceptRanges[x>>4]
	if n < sz {
		return RuneError, 1
	}
	b1 := s[1]
	if b1 < accept.lo || accept.hi < b1 {
		return RuneError, 1
	}
	if sz <= 2 {
		return rune(s0&mask2)<<6 | rune(b1&maskx), 2
	}
	b2 := s[2]
	if b2 < locb || hicb < b2 {
		return RuneError, 1
	}
	if sz <= 3 {
		return rune(s0&mask3)<<12 | rune(b1&maskx)<<6 | rune(b2&maskx), 3
	}
	b3 := s[3]
	if b3 < locb || hicb < b3 {
		return RuneError, 1
	}
	return rune(s0&mask4)<<18 | rune(b1&maskx)<<12 | rune(b2&maskx)<<6 | rune(b3&maskx), 4
}

// DecodeLastRune unpacks the last UTF-8 encoding in p and returns the rune
// and its width in bytes, with the same error results as DecodeRune.
func DecodeLastRune(p []byte) (r rune, size int) { return DecodeLastRuneInString(str(p)) }

// DecodeLastRuneInString is like DecodeLastRune but its input is a string.
// It looks at bytes, which for DecodeLastRune need not be valid UTF-8.
func DecodeLastRuneInString(s string) (rune, int) {
	end := len(s)
	if end == 0 {
		return RuneError, 0
	}
	start := end - 1
	r := rune(s[start])
	if r < RuneSelf {
		return r, 1
	}
	// Guard against O(n^2) behavior when traversing backwards through
	// strings with long sequences of invalid UTF-8.
	lim := max(end-UTFMax, 0)
	for start--; start >= lim; start-- {
		if RuneStart(s[start]) {
			break
		}
	}
	if start < 0 {
		start = 0
	}
	r, size := DecodeRuneInString(rt.SliceBytewise(s, start, end))
	if start+size != end {
		return RuneError, 1
	}
	return r, size
}

// RuneLen returns the number of bytes in the UTF-8 encoding of the rune. It
// returns -1 if the rune is not a valid value to encode in UTF-8.
func RuneLen(r rune) int {
	switch {
	case r < 0:
		return -1
	case r <= rune1Max:
		return 1
	case r <= rune2Max:
		return 2
	case surrogateMin <= r && r <= surrogateMax:
		return -1
	case r <= rune3Max:
		return 3
	case r <= MaxRune:
		return 4
	}
	return -1
}

// EncodeRune writes into p (which must be large enough) the UTF-8 encoding
// of the rune. If the rune is out of range, it writes the encoding of
// RuneError. It returns the number of bytes written.
func EncodeRune(p []byte, r rune) int {
	switch i := uint32(r); {
	case i <= rune1Max:
		p[0] = byte(r)
		return 1
	case i <= rune2Max:
		_ = p[1]
		p[0] = t2 | byte(r>>6)
		p[1] = tx | byte(r)&maskx
		return 2
	case i < surrogateMin, surrogateMax < i && i <= rune3Max:
		_ = p[2]
		p[0] = t3 | byte(r>>12)
		p[1] = tx | byte(r>>6)&maskx
		p[2] = tx | byte(r)&maskx
		return 3
	case i > rune3Max && i <= MaxRune:
		_ = p[3]
		p[0] = t4 | byte(r>>18)
		p[1] = tx | byte(r>>12)&maskx
		p[2] = tx | byte(r>>6)&maskx
		p[3] = tx | byte(r)&maskx
		return 4
	default:
		_ = p[2]
		p[0] = runeErrorByte0
		p[1] = runeErrorByte1
		p[2] = runeErrorByte2
		return 3
	}
}

// AppendRune appends the UTF-8 encoding of r to dst, a *vec.Vec[byte] (which
// grows) or a *vec.Fixed[byte] (which keeps what fits). If the rune is out of
// range, it appends the encoding of RuneError.
func AppendRune[D vec.Appender[byte]](dst D, r rune) {
	if uint32(r) <= rune1Max {
		dst.Push(byte(r))
		return
	}
	var buf [UTFMax]byte
	n := EncodeRune(buf[:], r)
	dst.Append(buf[:n]...)
}

// RuneCount returns the number of runes in p. Erroneous and short encodings
// are treated as single runes of width 1 byte.
func RuneCount(p []byte) int { return RuneCountInString(str(p)) }

// RuneCountInString is like RuneCount but its input is a string. For a
// string known to be ASCII it is len(s).
func RuneCountInString(s string) int {
	if rt.KnownASCII(s) {
		return len(s)
	}
	n := 0
	for i := 0; i < len(s); {
		if s[i] < RuneSelf {
			i++
		} else {
			_, size := DecodeRuneInString(rt.SliceBytewise(s, i, len(s)))
			i += size
		}
		n++
	}
	return n
}

// RuneStart reports whether the byte could be the first byte of an encoded,
// possibly invalid rune. Second and subsequent bytes always have the top two
// bits set to 10.
func RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// Valid reports whether p consists entirely of valid UTF-8-encoded runes.
func Valid(p []byte) bool { return ValidString(str(p)) }

// ValidString reports whether s consists entirely of valid UTF-8-encoded
// runes. Every string is, except views of other bytes made with
// unsafe.String, which Valid uses.
func ValidString(s string) bool {
	if rt.KnownASCII(s) {
		return true
	}
	for i := 0; i < len(s); {
		si := s[i]
		if si < RuneSelf {
			i++
			continue
		}
		x := first[si]
		if x == xx {
			return false // illegal starter byte
		}
		size := int(x & 7)
		if i+size > len(s) {
			return false // short or invalid
		}
		accept := acceptRanges[x>>4]
		if c := s[i+1]; c < accept.lo || accept.hi < c {
			return false
		} else if size == 2 {
		} else if c := s[i+2]; c < locb || hicb < c {
			return false
		} else if size == 3 {
		} else if c := s[i+3]; c < locb || hicb < c {
			return false
		}
		i += size
	}
	return true
}

// ValidRune reports whether r can be legally encoded as UTF-8. Code points
// that are out of range or a surrogate half are illegal.
func ValidRune(r rune) bool {
	switch {
	case 0 <= r && r < surrogateMin:
		return true
	case surrogateMax < r && r <= MaxRune:
		return true
	}
	return false
}
