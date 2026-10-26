package main
import "pluto/sync"

type Item struct{ n int }
type Result struct{ sq int }

func process(items []Item, results []Result) {
	var s sync.Scope
	for i, it := range items {
		s.Task(func() { results[i] = Result{it.n * it.n} })
	}
	s.Wait()
}

func main() { process(nil, nil) }