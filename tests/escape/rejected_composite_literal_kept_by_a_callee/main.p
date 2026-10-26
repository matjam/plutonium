package main

type T struct{ n int }
func put(ps []*T, i int, p *T) { ps[i] = p }
func main() {
	var ps [3]*T
	for i := 0; i < 3; i++ {
		put(ps[:], i, &T{i})
	}
	println(ps[0].n)
}
