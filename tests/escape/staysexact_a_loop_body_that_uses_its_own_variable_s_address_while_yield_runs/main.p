package main

func seq(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}
func use(p *int) int { return *p }
func f() int {
	sum := 0
	for x := range seq {
		y := x
		sum += use(&y)
	}
	return sum
}
func main() { println(f()) }
