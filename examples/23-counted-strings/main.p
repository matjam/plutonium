package main

import "pluto/fmt"

// Strings are immutable, valid UTF-8, and reference counted.
//
// A string made at run time, such as the result of "+", lives in a buffer
// that counts the strings using it. Copying a string adds one to the count,
// dropping a copy subtracts one, and the last drop frees the buffer. Slicing
// shares the buffer. There is no garbage collector and no call to free; a
// string copies as freely as an integer (language spec sections 7.3 and
// 18.8).
//
// Because every string is valid UTF-8, a slice expression must start and end
// on a rune boundary. Cutting a rune in half panics instead of producing a
// torn string. This is the one place a string slice behaves differently from
// Go's, and the panic message names the library functions that find a safe
// boundary.

func greet(name string) string {
	return "hello, " + name + "!" // allocates one counted buffer
}

func main() {
	s := greet("wörld")
	t := s // copies the header and shares the bytes: the count is now 2
	fmt.Println(t, len(t), "bytes")

	// range yields runes; the index is the byte offset where each begins.
	for i, r := range t {
		if r > 127 {
			fmt.Printf("rune %c is at byte %d and takes %d bytes\n", r, i, len(string(r)))
		}
	}

	greeting := t[:7] // shares t's buffer and keeps it alive
	t = ""            // drops one copy; the buffer is still held
	fmt.Printf("%q\n", greeting)

	// The o-umlaut occupies bytes 8 and 9, so s[:9] would cut it in half
	// and panic. Slice at rune boundaries instead: byte 7 starts "w", byte
	// 8 starts the o-umlaut and byte 10 starts "r".
	word := s[7:10]
	runes := 0
	for range word {
		runes++
	}
	fmt.Printf("%q is %d runes in %d bytes\n", word, runes, len(word))
}
