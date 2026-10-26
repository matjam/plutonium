package main
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func incLocked() { n++ }

func work(done chan bool) {
	mu.Lock()
	incLocked()
	incLocked()
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}