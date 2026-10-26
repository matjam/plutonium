package main
import "pluto/sync"

type T owner struct{ n int }

func main() {
	var mu sync.Mutex
	o := new(T)
	k := new(T)
	q := &o.n
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		k = new(T)
		mu.Unlock()
	})
	println(*q)
	s.Wait()
	println(k.n)
}