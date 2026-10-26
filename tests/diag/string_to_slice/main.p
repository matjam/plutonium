package main

// []byte(s) and []rune(s) are not permitted: the copy would belong to no
// one (language spec section 3). Each form gets the fix for its idiom.

func copyBytes(s string) int {
	b := []byte(s) // assigned to a name: strings.Bytes, or copy into a buffer
	return len(b)
}

func runes(s string) int {
	r := []rune(s) // assigned to a name: range, or strings.Runes
	return len(r)
}

func count(s string) int {
	return len([]rune(s)) // counting: utf8.RuneCountInString
}

func walk(s string) int {
	n := 0
	for _, b := range []byte(s) { // a range over bytes: index the string
		n += int(b)
	}
	for _, r := range []rune(s) { // a range over runes: range over the string
		n += int(r)
	}
	return n
}

func literal() []byte {
	return []byte("header") // a constant string
}

func main() {
	println(copyBytes("a"), runes("b"), count("c"), walk("d"), len(literal()))
}
