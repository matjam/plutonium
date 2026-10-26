package main
import "pluto/sync"

func main() {
	var grid [3][4]int
	var s sync.Scope
	for y := range grid {
		for x := range 4 {
			s.Task(func() { grid[y][x] = y*4 + x })
		}
	}
	s.Wait()
	println(grid[2][3])
}