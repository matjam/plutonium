package main
func sum(xs []int, out chan int) {
	t := 0
	for _, x := range xs {
		t += x
	}
	out <- t
}

func main() {
	xs := new([3]int)[:]
	out := make(chan int)
	task sum(xs, out)
	xs[0] = 5
	println(<-out)
}