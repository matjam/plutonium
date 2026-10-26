package main
import "pluto/sync"

func crawl(urls []string) (int, error) {
	jobs := make(chan string, 16)
	var mu sync.Mutex
	var total int
	var first error
	var s sync.Scope
	for w := 0; w < 8; w++ {
		s.Task(func() {
			for u := range jobs {
				mu.Lock()
				total += len(u)
				if first == nil && u == "" {
					first = nil
				}
				mu.Unlock()
			}
		})
	}
	for _, u := range urls {
		jobs <- u
	}
	close(jobs)
	s.Wait()
	return total, first
}

func main() { crawl(nil) }