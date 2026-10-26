package main
import "pluto/sync"

func sortPar(v []int) {
	if len(v) < 2 {
		return
	}
	p := len(v) / 2
	var s sync.Scope
	s.Task(func() { sortPar(v[:p]) })
	sortPar(v[p+1:])
	v[p]++
}

func main() { sortPar(nil) }