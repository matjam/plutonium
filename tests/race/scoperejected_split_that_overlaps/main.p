package main
import "pluto/sync"

func zero(v []int) {
	for i := range v {
		v[i] = 0
	}
}

func run(v []int, p int) {
	var s sync.Scope
	s.Task(func() { zero(v[:p]) })
	zero(v[p-1:])
}

func main() { run(nil, 0) }