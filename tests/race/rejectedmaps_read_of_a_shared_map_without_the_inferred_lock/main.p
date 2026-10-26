package main
import "pluto/sync"

var (
	mu    sync.Mutex
	cache = map[string]int{}
)

func fill(done chan bool) {
	mu.Lock()
	cache["x"] = 1
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task fill(done)
	task fill(done)
	<-done
	<-done
	println(cache["x"])
}