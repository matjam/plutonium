package main
var table = []int{1, 2, 3}

func sum(xs []int, out chan int) {
	t := 0
	for _, x := range xs {
		t += x
	}
	out <- t
}

func main() {
	out := make(chan int)
	task sum(table, out)
	task sum(table[1:], out)
	println(<-out + <-out)
}