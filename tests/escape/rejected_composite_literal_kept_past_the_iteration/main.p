package main

type T struct{ n int }
func main() {
	var p *T
	for i := 0; i < 3; i++ {
		p = &T{i}
	}
	println(p.n)
}
