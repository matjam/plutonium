package main
import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	m  map[string]int
}

func (c *Counter) Add(k string) {
	c.mu.Lock()
	c.m[k]++
	c.mu.Unlock()
}

func (c *Counter) Get(k string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[k]
}

var counter = Counter{m: map[string]int{}}

func work(done chan bool) {
	counter.Add("x")
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
	println(counter.Get("x"))
}