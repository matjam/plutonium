package main

type W interface{ Write(p []byte) int }
type Dev[T ~uint8] struct{ data T }
func (d *Dev[T]) Write(v T) int { d.data = v; return 1 }
type sink struct{}
func (sink) Write(p []byte) int { return len(p) }
func put[V W](w V) int {
	var buf [4]byte
	return w.Write(buf[:])
}
func main() {
	var d Dev[uint8]
	d.Write(1)
	println(put(sink{}))
}
