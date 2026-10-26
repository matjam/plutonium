package main

func apply(f func()) { f() }
func main() {
	n := 0
	apply(func() { n++ })
	_ = n
}
