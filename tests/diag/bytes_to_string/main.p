package main

// string(b) of a byte slice is not permitted: a string holds valid UTF-8,
// so the conversion would have to change the bytes (language spec section
// 6.10). Each form gets the fix with its own argument.

func name(b []byte) string {
	return string(b) // a returned value
}

func fill(buf *[16]byte, n int) string {
	return string(buf[:n]) // a slice of an array
}

func lines(data [][]byte) int {
	n := 0
	for _, line := range data {
		text := string(line) // assigned to a name
		n += len(text)
	}
	return n
}

func main() {
	var raw [8]byte
	println(string(raw[:4]), name(raw[:]), fill(&[16]byte{}, 3), lines([][]byte{raw[:]}))
}
