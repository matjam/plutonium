package main

import (
	"pluto/fmt"
	"pluto/strings"
	"pluto/unicode/utf8"
)

// strings.Truncate and utf8.RuneStartBefore round a byte count down to a
// rune boundary (language spec sections 7.3 and F.6), the replacement for
// the s[:n] of Go.

type Name string

func main() {
	s := "añ€😀z" // widths 1, 2, 3, 4, 1: offsets 0, 1, 3, 6, 10; length 11
	for n := -1; n <= 13; n++ {
		t := strings.Truncate(s, n)
		fmt.Printf("n=%d start=%d truncate=%q len=%d\n", n, utf8.RuneStartBefore(s, n), t, len(t))
	}
	fmt.Println(strings.Truncate("", 3) == "", strings.Truncate("abc", 3), strings.Truncate("abc", 0) == "")
	fmt.Println(utf8.RuneStartBefore("", 0), utf8.RuneStartBefore("", 5), utf8.RuneStartBefore("abc", 2))

	// The result is a valid bound for strings.Slice.
	for n := 0; n <= len(s); n++ {
		i := utf8.RuneStartBefore(s, n)
		fmt.Print(strings.Slice(s, 0, i), "|", strings.SliceFrom(s, i), " ")
	}
	fmt.Println()

	// Truncate of a long text keeps whole runes, whatever the limit.
	title := "Привет, мир! Ünïcödé ✓"
	for _, limit := range []int{0, 1, 2, 3, 7, 12, 40} {
		fmt.Printf("%d %q\n", limit, strings.Truncate(title, limit))
	}
	fmt.Println(strings.Slice(Name("añ€"), 1, 3), strings.SliceFrom(Name("añ€"), 3))
}
