package main

// Every string holds valid UTF-8 (language spec section 6.10).

const magic = "\x7fELF\xff"

func main() {
	ok := "café \xc3\xa9" // valid: the escapes spell UTF-8
	println(magic, ok, "\xed\xa0\x80")
}
