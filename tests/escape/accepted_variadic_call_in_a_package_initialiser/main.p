package main

func all(xs ...int) func() []int {
	return func() []int { return xs }
}
var F = all(1, 2)
func main() { println(len(F())) }
