// strings.SliceFrom must start between runes (language spec section 7.3):
// offset 2 of "héllo" is inside the é.
package main

import "pluto/strings"

func rest(s string, i int) string { return strings.SliceFrom(s, i) }

func main() {
	s := "héllo"
	println(rest(s, 0), rest(s, 1), rest(s, 3))
	println(rest(s, 2))
}
