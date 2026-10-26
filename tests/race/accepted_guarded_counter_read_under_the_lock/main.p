package main
import "pluto/sync"

var (
	mu    sync.Mutex
	count int
	wg    sync.WaitGroup
)

func add() {
	mu.Lock()
	count++
	mu.Unlock()
	wg.Done()
}

func main() {
	wg.Add(2)
	task add()
	task add()
	wg.Wait()
	mu.Lock()
	println(count)
	mu.Unlock()
}