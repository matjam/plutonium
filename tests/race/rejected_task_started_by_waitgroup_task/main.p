package main
import "pluto/sync"

var (
	wg sync.WaitGroup
	n  int
)

func bump() { n++ }

func main() {
	for i := 0; i < 3; i++ {
		wg.Task(bump)
	}
	wg.Wait()
}