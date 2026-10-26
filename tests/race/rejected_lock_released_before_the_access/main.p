package main
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func bump(done chan bool) {
	mu.Lock()
	n++
	mu.Unlock()
	n++
	done <- true
}

func main() {
	done := make(chan bool)
	task bump(done)
	task bump(done)
	<-done
	<-done
}