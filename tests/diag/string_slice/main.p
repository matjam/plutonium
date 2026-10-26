package main

import "pluto/strings"

// A slice expression on a string is not permitted: a cut at any byte could
// split a rune, so strings.Slice and strings.SliceFrom check the bounds
// (language spec section 11.12). Arrays and slices keep a[i:j].

type Name string

const greeting = "héllo, wörld"

func both(s string, i, j int) string {
	return s[i:j] // typed, two bounds
}

func from(s string, i int) string {
	return s[i:] // one bound
}

func head(title string) string {
	return title[:40] // the bound of a prefix
}

func all(s string) string {
	return s[:] // no bound
}

func named(n Name, i int) Name {
	return n[i:] // a defined string type
}

func untyped() string {
	return "héllo"[1:3] // an untyped constant
}

func constant() string {
	return greeting[:5] // a typed constant
}

func generic[S ~string](s S) S {
	return s[1:] // a type parameter whose type set holds strings
}

func three(s string) string {
	return s[0:1:2] // three indexes
}

func ok(a [4]int, b []byte, s string) int {
	x := a[1:3]
	y := b[1:]
	return len(x) + len(y) + len(strings.Slice(s, 0, 0)) + len(s)
}

func main() {
	println(both("a", 0, 1), from("a", 0), head("a"), all("a"), named("a", 0), untyped(), constant(), generic("a"), three("abc"))
	println(ok([4]int{}, nil, "d"))
}
