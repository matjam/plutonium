package main
import "pluto/sync"

func fill(grid [][]int) {
	var s sync.Scope
	for y := range grid {
		s.Task(func() { grid[y][0] = y })
	}
}

func main() { fill(nil) }