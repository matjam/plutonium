package main
import "pluto/sync"

func first(items []int) int {
	var results [4]int
	var s sync.Scope
	for i := range results {
		s.Task(func() { results[i] = items[i] })
	}
	return results[0]
}

func main() { println(first([]int{1, 2, 3, 4})) }