package main

func sum(done chan int, xs ...int) {
	t := 0
	for _, x := range xs {
		t += x
	}
	done <- t
}
func main() {
	d := make(chan int)
	task sum(d, 1, 2, 3)
	println(<-d)
}
