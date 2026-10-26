package main
import "pluto/sync"

var (
	rw    sync.RWMutex
	table [4]int
)

func reader(done chan bool) {
	rw.RLock()
	_ = table[0]
	rw.RUnlock()
	done <- true
}

func writer(done chan bool) {
	rw.RLock()
	table[0] = 2
	rw.RUnlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task reader(done)
	task writer(done)
	<-done
	<-done
}