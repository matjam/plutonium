package main

// Slicing an array through a nil pointer panics, however small the array:
// the slice would have a length and no memory behind it.

func view(p *[4]byte) []byte {
	return p[1:3]
}

func main() {
	var a [4]byte
	println(len(view(&a)))
	var none *[4]byte
	println(len(view(none)))
}
