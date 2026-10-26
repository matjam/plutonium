package main
import "pluto/sync/atomic"

var hits atomic.Int64

func handle(done chan bool) {
	hits.Add(1)
	done <- true
}

func main() {
	done := make(chan bool)
	task handle(done)
	task handle(done)
	<-done
	<-done
	println(hits.Load())
}