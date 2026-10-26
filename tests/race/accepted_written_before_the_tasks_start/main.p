package main
var limit int

func use(done chan int) { done <- limit }

func main() {
	limit = 10
	done := make(chan int)
	task use(done)
	task use(done)
	println(<-done + <-done)
}