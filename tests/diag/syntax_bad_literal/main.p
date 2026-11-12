package main

// A string literal that does not end on its line (E1008).

func main() {
	s := "unterminated
	println(s)
}
