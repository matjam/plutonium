package main

// string(i) of an integer other than a rune is not permitted: Go makes the
// character with that code, where programs usually want the digits
// (language spec section 6.10). A rune and an untyped constant stay.

const big int = 65

func label(n int) string {
	return "item " + string(n) // int: strconv.Itoa
}

func letter(b byte) string {
	return string(b) // byte: the character, or the digits
}

func wide(w int64, u uint16) (string, string) {
	return string(w), string(u) // other integer types
}

func small(s int16, big uint64) (string, string) {
	return string(s), string(big) // signed and unsigned: FormatInt, FormatUint
}

func ok(r rune, i int) string {
	return string(r) + string(65) + string('B') + string(rune(i)) // all accepted
}

func main() {
	println(label(1), letter(2), big, ok('a', 3))
	a, b := wide(4, 5)
	c, d := small(6, 7)
	println(a, b, c, d, string(big))
}
