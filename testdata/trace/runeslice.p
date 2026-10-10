// A string slice must start and end between runes (language spec section
// 6.10): s[0:2] of "héllo" cuts the é in half.
package main

func cut(s string, i, j int) string { return s[i:j] }

func main() {
	s := "héllo"
	println(cut(s, 0, 1), cut(s, 1, 3), cut(s, 3, 6))
	println(cut(s, 0, 2))
}
