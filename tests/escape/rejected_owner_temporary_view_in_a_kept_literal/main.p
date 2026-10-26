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
type H struct{ s []int }
func main() {
	h := &H{s: readAll(1).Slice()}
	_ = h
}
