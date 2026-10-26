package main
import "pluto/sync"

var (
	a, b sync.Mutex
	n    int
)

func one(done chan bool) {
	a.Lock()
	n++
	a.Unlock()
	done <- true
}

func two(done chan bool) {
	b.Lock()
	n++
	b.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task one(done)
	task two(done)
	<-done
	<-done
}