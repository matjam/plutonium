package main

// checksum is written in assembly, but the package has no .s file.
func checksum(p *byte, n int) uint32

func main() {
	var b [4]byte
	println(checksum(&b[0], len(b)))
}
