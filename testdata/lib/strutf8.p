package main

// Strings hold valid UTF-8 (language spec section 6.10): conversions
// replace what is not valid, FromUTF8 refuses it, and the ASCII flag that
// constructors compute gives the library fast paths.

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/rt"
	"pluto/strings"
	"pluto/unicode/utf8"
	"unsafe"
)

func main() {
	// string(b) replaces each byte that does not begin a valid sequence.
	for _, b := range [][]byte{
		{'a', 'b'}, {0xff}, {'a', 0xe4, 0xb8, 'b'}, {0xed, 0xa0, 0x80}, {0xc3, 0xa9},
		{0xf0, 0x9f, 0x98, 0x80}, {0xf0, 0x9f, 0x98}, {}, {0xc0, 0x80},
	} {
		s := string(b)
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
	head := []byte(both[:5])
	fmt.Println(rt.KnownASCII(both[:5]), rt.KnownASCII(string(head)), rt.KnownASCII(strings.Repeat("ab", 3)))
	freeSlice(head)
	fmt.Println(utf8.RuneCountInString(ascii+ascii), utf8.RuneCountInString(both), len(both))

	// Slicing between runes works on any string; byte offsets that
	// strings.Index returns are rune boundaries.
	i := strings.Index(both, "é")
	fmt.Printf("%q %q %q %d\n", both[:i], both[i:], both[i+len("é"):], i)
	for k, r := range both[3:] {
		fmt.Print(k, ":", string(r), " ")
	}
	fmt.Println()

	// []byte(s) and []rune(s) copy into memory from the current allocator,
	// as make does; the program frees it.
	bs := []byte(accent)
	bs[0] = 'C'
	runes := []rune(accent)
	fmt.Println(accent, string(bs), len(runes), len(bs), string(runes[3]))
	freeSlice(bs)
	freeSlice(runes)
}

// freeSlice returns the array of a slice made by a conversion (or make) to
// the default allocator.
func freeSlice[T any](s []T) {
	var zero T
	alloc.Default().Free(unsafe.Pointer(unsafe.SliceData(s)), uintptr(cap(s))*unsafe.Sizeof(zero), unsafe.Alignof(zero))
}
