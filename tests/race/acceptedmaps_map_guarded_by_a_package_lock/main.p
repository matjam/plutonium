package main
import "pluto/sync"

var (
	mu    sync.Mutex
	cache = map[string]int{}
)

func fill(k string, done chan bool) {
	mu.Lock()
	cache[k]++
	mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task fill("a", done)
	task fill("b", done)
	<-done
	<-done
	mu.Lock()
	println(len(cache), cache["a"])
	for k, v := range cache {
		println(k, v)
	}
	mu.Unlock()
}