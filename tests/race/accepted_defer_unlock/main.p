package main
import "pluto/sync"

var (
	mu sync.Mutex
	m  [8]int
)

func put(i, v int, done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	m[i] = v
	done <- true
}

func get(i int) int {
	mu.Lock()
	defer mu.Unlock()
	return m[i]
}

func main() {
	done := make(chan bool)
	task put(1, 2, done)
	task put(2, 3, done)
	<-done
	<-done
	println(get(1) + get(2))
}