package main

import (
	"pluto/fmt"
	"pluto/unicode/utf8"
)

var samples = []string{"", "a", "héllo", "日本語", "\xff", "\xe6\x97", "a\xc0\x80b", "😀x", "\xed\xa0\x80", "\xf4\x90\x80\x80", "ab\xf0\x9f\x98"}

func main() {
	for _, s := range samples {
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
	out := make([]byte, 0, 128)
	for _, r := range runes {
		n := utf8.EncodeRune(buf[:], r)
		out = utf8.AppendRune(out, r)
		fmt.Printf("%d: len=%d enc=%d %x valid=%t\n", r, utf8.RuneLen(r), n, buf[:n], utf8.ValidRune(r))
	}
	fmt.Printf("%x\n", out)
	fmt.Printf("%t %t\n", utf8.RuneStart(0x80), utf8.RuneStart('a'))
}
