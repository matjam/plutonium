package main
import "pluto/sync"

func main() {
	var grid [3][3]int
	var s sync.Scope
	for y := range grid {
		s.Task(func() {
			var inner sync.Scope
			for x := range grid[y] {
				inner.Task(func() { grid[y][x] = y + x })
			}
		})
	}
	s.Wait()
	println(grid[1][1])
}