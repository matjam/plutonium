package main

type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
func main() {
	println(sum(readAll(3).Slice()), len(readAll(2).Slice()), *readAll(1).Ptr())
	if s := readAll(3).Slice(); len(s) > 0 {
		println(s[0])
	}
	switch s := readAll(2).Slice(); len(s) {
	case 2:
		println(s[1])
	}
	for _, v := range readAll(2).Slice() {
		println(v)
	}
}
