package main
import "pluto/sync"

func Map[T, U any](in []T, out []U, f func(T) U) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = f(in[i]) })
	}
}

func main() {
	var lens [2]int
	count := 0
	Map([]string{"a", "b"}, lens[:], func(s string) int { count++; return len(s) })
	println(count)
}