package main

func f() []byte {
	var buf [16]byte
	return buf[:]
}
func main() { _ = f() }
