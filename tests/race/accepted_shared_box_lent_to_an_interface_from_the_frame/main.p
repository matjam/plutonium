package main
import "pluto/alloc"

func show(a any) {}

func take(c chan any, done chan bool) {
	<-c
	done <- true
}

func main() {
	sh := alloc.Shared[int]()
	show(sh)
	c := make(chan any, 1)
	done := make(chan bool)
	task take(c, done)
	c <- 1
	<-done
}