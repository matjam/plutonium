package main
import "pluto/sync"

func count(counts []int) int {
	var total int
	var s sync.Scope
	for _, n := range counts {
		s.Task(func() { total += n })
	}
	s.Wait()
	return total
}

func main() { println(count([]int{1, 2})) }