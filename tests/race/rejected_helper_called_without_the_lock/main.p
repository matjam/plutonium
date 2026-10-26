package main
import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func incLocked() { n++ }

func good(done chan bool) {
	mu.Lock()
	incLocked()
	mu.Unlock()
	done <- true
}

func bad(done chan bool) {
	incLocked()
	done <- true
}

func main() {
	done := make(chan bool)
	task good(done)
	task bad(done)
	<-done
	<-done
}