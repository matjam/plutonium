package main

import "pluto/strings"

// A string is cut by strings.Slice and strings.SliceFrom (language spec
// sections 7.3 and 11.12). Go runs this program with the calls replaced by
// the slice expressions; every bound is a rune boundary, where they agree.

type Name string

func middle[S ~string](s S) S { return strings.Slice(s, 1, len(s)-1) }

func tail[S ~string](s S, n int) S { return strings.SliceFrom(s, n) }

func main() {
	s := "héllo, wörld"
	println(strings.Slice(s, 0, 1), strings.Slice(s, 1, 3), strings.Slice(s, 3, len(s)))
	println(strings.SliceFrom(s, 0), strings.SliceFrom(s, 3), strings.SliceFrom(s, len(s)) == "")
	println(strings.Slice(s, 2+1, 6) == "llo")

	// Bounds from a search are always boundaries.
	i := strings.Index(s, "ö")
	println(i, strings.Slice(s, 0, i), strings.SliceFrom(s, i+len("ö")))

	// Constant operands, typed and untyped, give a non-constant string.
	const c = "constant ✓ string"
	println(strings.Slice(c, 0, 8), strings.SliceFrom(c, 9), strings.Slice("abc", 1, 2))

	// A named string type is sliced as the named type.
	n := Name("héllo")
	var m Name = strings.SliceFrom(n, 1)
	println(m, middle(n), middle("x✓y"), tail(Name("añb"), 1))

	// The substring is a string like any other: it is kept and compared.
	parts := []string{strings.Slice(s, 0, 1), strings.Slice(s, 1, 3), strings.SliceFrom(s, 3)}
	joined := ""
	for _, p := range parts {
		joined += p
	}
	println(joined == s, len(joined))
	w := s
	for len(w) > 0 {
		_, size := decode(w)
		w = strings.SliceFrom(w, size)
		print(size, " ")
	}
	println()
}

// decode returns the first rune of s and its width.
func decode(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return 0, i
		}
		_ = r
	}
	return 0, len(s)
}
