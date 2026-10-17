package main

// When main returns the program ends, as in Go (language spec section
// 15.4): tasks that are still running are not waited for, whether they
// compute, wait for a channel that nothing sends on, or start tasks of
// their own.

func spin() {
	for {
	}
}

func block(c chan int) {
	<-c
}

func parent(c chan int) {
	task block(c)
	task spin()
}

func main() {
	c := make(chan int)
	task spin()
	task block(c)
	task parent(c)
	println("main returns")
}
