package main

type Scanner struct {
	src []byte
	pos int
}
func (s *Scanner) Init(src []byte) { s.src = src; s.pos = 0 }
func main() {
	var buf [8]byte
	var s Scanner
	s.Init(buf[:])
}
