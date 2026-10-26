package main
import "pluto/sync"

func work(wg *sync.WaitGroup, mu *sync.Mutex) {
	mu.Lock()
	mu.Unlock()
	wg.Done()
}

var (
	wg sync.WaitGroup
	mu sync.Mutex
)

func main() {
	wg.Add(2)
	task work(&wg, &mu)
	task work(&wg, &mu)
	wg.Wait()
}