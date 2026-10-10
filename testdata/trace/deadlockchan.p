package main

// A deadlock prints the stack of every parked puroutine, with what it waits
// for, in the order the puroutines were started. The frames of the runtime
// that park it are left out.

func receive(c chan int) int {
	return <-c
}

func middle(c chan int) {
	println(receive(c))
}

func send(c chan int) {
	c <- 1
}

func choose(a, b chan int) {
	select {
	case v := <-a:
		println(v)
	case b <- 2:
	}
}

func nilrecv() {
	var c chan int
	<-c
}

func nilsend() {
	var c chan int
	c <- 1
}

func nocases() {
	select {}
}

func main() {
	a := make(chan int)
	b := make(chan int)
	c := make(chan int)
	d := make(chan int)
	done := make(chan int)
	go middle(a)
	go send(b)
	go choose(c, d)
	go nilrecv()
	go nilsend()
	go nocases()
	<-done
}
