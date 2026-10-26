package main
import "pluto/sync"

var (
	rw    sync.RWMutex
	table [4]int
)

func reader(done chan int) {
	rw.RLock()
	v := table[0]
	rw.RUnlock()
	done <- v
}

func writer(done chan int) {
	rw.Lock()
	table[0]++
	rw.Unlock()
	done <- 0
}

func main() {
	done := make(chan int)
	task reader(done)
	task writer(done)
	<-done
	<-done
}