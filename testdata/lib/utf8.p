package main

import (
	"pluto/fmt"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

// The samples include bytes that are not valid UTF-8, which no string may
// hold (language spec section 6.10): they are bytes, and the string
// functions see them through unsafe.String, to check that the decoder
// copes with them.
var samples = [][]byte{
	{}, {'a'}, {'h', 0xc3, 0xa9, 'l', 'l', 'o'}, {0xe6, 0x97, 0xa5, 0xe6, 0x9c, 0xac, 0xe8, 0xaa, 0x9e},
	{0xff}, {0xe6, 0x97}, {'a', 0xc0, 0x80, 'b'}, {0xf0, 0x9f, 0x98, 0x80, 'x'}, {0xed, 0xa0, 0x80},
	{0xf4, 0x90, 0x80, 0x80}, {'a', 'b', 0xf0, 0x9f, 0x98},
}

func main() {
	for _, sb := range samples {
		s := unsafe.String(unsafe.SliceData(sb), len(sb))
		r, n := utf8.DecodeRuneInString(s)
		lr, ln := utf8.DecodeLastRuneInString(s)
		b := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		k := copy(b, s)
		r2, n2 := utf8.DecodeRune(b[:k])
		fmt.Printf("%x: dec=%d,%d last=%d,%d b=%d,%d count=%d %d valid=%t %t full=%t %t\n", s, r, n, lr, ln, r2, n2,
			utf8.RuneCountInString(s), utf8.RuneCount(b[:k]), utf8.ValidString(s), utf8.Valid(b[:k]),
			utf8.FullRuneInString(s), utf8.FullRune(b[:k]))
	}
	runes := []rune{0, 'a', 0x7f, 0x80, 0x7ff, 0x800, 0xffff, 0x10000, 0x10ffff, 0x110000, -1, 0xd800, 0xdfff, 'é', '世'}
	var buf [8]byte
	var mem [128]byte
	out := vec.NewFixed(mem[:])
	for _, r := range runes {
		n := utf8.EncodeRune(buf[:], r)
		utf8.AppendRune(&out, r)
		fmt.Printf("%d: len=%d enc=%d %x valid=%t\n", r, utf8.RuneLen(r), n, buf[:n], utf8.ValidRune(r))
	}
	fmt.Printf("%x %t\n", out.Slice(), out.Truncated())
	var small [4]byte
	f := vec.NewFixed(small[:])
	utf8.AppendRune(&f, '世')
	utf8.AppendRune(&f, 'é')
	fmt.Printf("%x %t\n", f.Slice(), f.Truncated())
	fmt.Printf("%t %t\n", utf8.RuneStart(0x80), utf8.RuneStart('a'))
}
