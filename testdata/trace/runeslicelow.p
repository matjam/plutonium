// strings.Slice checks the low bound as well as the high one (language spec
// section 7.3): s[2:3] of "héllo" starts inside the é.
package main

import "pluto/strings"

func cut(s string, i, j int) string { return strings.Slice(s, i, j) }

func main() {
	s := "héllo"
	println(cut(s, 0, 1), cut(s, 1, 3), cut(s, 3, 6), cut(s, 3, 3) == "")
	println(cut(s, 2, 3))
}
