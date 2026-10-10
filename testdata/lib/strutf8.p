package main

// Strings hold valid UTF-8 (language spec section 6.10): FromUTF8Lossy replaces
// what is not valid, FromUTF8 refuses it, and the ASCII flag that
// constructors compute gives the library fast paths.

import (
	"pluto/fmt"
	"pluto/rt"
	"pluto/strings"
	"pluto/unicode/utf8"
)

func main() {
	// strings.FromUTF8Lossy replaces each byte that does not begin a valid
	// sequence.
	for _, b := range [][]byte{
		{'a', 'b'}, {0xff}, {'a', 0xe4, 0xb8, 'b'}, {0xed, 0xa0, 0x80}, {0xc3, 0xa9},
		{0xf0, 0x9f, 0x98, 0x80}, {0xf0, 0x9f, 0x98}, {}, {0xc0, 0x80},
	} {
		s := strings.FromUTF8Lossy(b)
		_, err := strings.FromUTF8(b)
		fmt.Printf("%x -> %q valid=%t runes=%d ascii=%t fromutf8=%v\n", b, s, utf8.ValidString(s), utf8.RuneCountInString(s), rt.KnownASCII(s), err)
	}

	// string(runes) and string(r) replace invalid code points.
	rs := []rune{'a', 0xd800, -1, 0x110000, 'é', 0x1F600}
	fmt.Printf("%q\n", string(rs))
	for _, r := range rs {
		fmt.Printf("%q ", string(r))
	}
	fmt.Println()
	var wide int64 = 0x1_0000_0041 // not a code point, though its low bits are 'A'
	fmt.Printf("%q %q %q\n", string(wide), string(rune(wide)), string(rune('A')))

	// The ASCII flag: literals, conversions, concatenation and substrings.
	ascii, accent := "plain", "café"
	both := ascii + accent
	fmt.Println(rt.KnownASCII(ascii), rt.KnownASCII(accent), rt.KnownASCII(ascii+ascii), rt.KnownASCII(both))
	head := strings.Bytes(both[:5])
	fmt.Println(rt.KnownASCII(both[:5]), rt.KnownASCII(strings.FromUTF8Lossy(head.Slice())), rt.KnownASCII(strings.Repeat("ab", 3)))
	head.Free()
	fmt.Println(utf8.RuneCountInString(ascii+ascii), utf8.RuneCountInString(both), len(both))

	// Slicing between runes works on any string; byte offsets that
	// strings.Index returns are rune boundaries.
	i := strings.Index(both, "é")
	fmt.Printf("%q %q %q %d\n", both[:i], both[i:], both[i+len("é"):], i)
	for k, r := range both[3:] {
		fmt.Print(k, ":", string(r), " ")
	}
	fmt.Println()

	// strings.Bytes and strings.Runes copy into vec.Vecs from the current
	// allocator, which own the copies and free them when they are dropped.
	// copy(buf[:], s) fills an array of the program's own instead.
	bs := strings.Bytes(accent)
	bs.Slice()[0] = 'C'
	runes := strings.Runes(accent)
	fmt.Println(accent, strings.FromUTF8Lossy(bs.Slice()), runes.Len(), bs.Len(), string(runes.At(3)))
	var buf [8]byte
	n := copy(buf[:], accent)
	fmt.Println(n, strings.FromUTF8Lossy(buf[:n]), rt.KnownASCII(strings.FromUTF8Lossy(buf[:n-2])))
}
