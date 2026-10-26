package main
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func try(done chan bool) {
	if mu.TryLock() {
		n++
		mu.Unlock()
	}
	done <- true
}

func main() {
	done := make(chan bool)
	task try(done)
	task try(done)
	<-done
	<-done
}