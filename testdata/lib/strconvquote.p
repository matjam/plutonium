package main

import (
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
	"pluto/vec"
)

// strconv's quoting into new strings and into each kind of destination.
// The expected output is Go's: every rune here is printable or an ASCII
// control, which Go escapes as Plutonium does.

func main() {
	samples := []string{
		"", "plain", "tab\there", "\"quoted\" and \\back\\", "bell\a bs\b ff\f nl\n cr\r vt\v del\x7f nul\x00",
		"héllo wörld 世界", strings.Repeat("ab\t", 30), strings.Repeat("x", 63) + "\n" + strings.Repeat("é", 40),
	}
	for _, s := range samples {
		q := strconv.Quote(s)
		v := vec.New[byte]()
		strconv.AppendQuote(&v, s)
		var mem [512]byte
		f := vec.NewFixed(mem[:])
		strconv.AppendQuote(&f, s)
		fmt.Println(q, len(q), string(v.Slice()) == q, string(f.Slice()) == q)
		v.Free()
	}
	var small [8]byte
	f := vec.NewFixed(small[:])
	strconv.AppendQuote(&f, "a\tb\tc\td")
	fmt.Println(string(f.Slice()), f.Truncated())
	for _, r := range []rune{'a', '\'', '\\', '\n', '\t', '\r', 'é', '世', '😀'} {
		fmt.Print(strconv.QuoteRune(r), " ")
	}
	fmt.Println()
}
