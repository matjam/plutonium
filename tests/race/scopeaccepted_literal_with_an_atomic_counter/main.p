package main
import (
	"pluto/sync"
	"pluto/sync/atomic"
)

func Map[T, U any](in []T, out []U, f func(T) U) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = f(in[i]) })
	}
}

func main() {
	var lens [2]int
	var calls atomic.Int64
	Map([]string{"a", "b"}, lens[:], func(s string) int { calls.Add(1); return len(s) })
	println(calls.Load())
}