package main
import (
	"pluto/fmt"
	"pluto/hash"
	"pluto/vec"
)

func produce(out chan<- vec.Vec[int]) {
	v := vec.New[int]()
	v.Push(1)
	out <- v
	close(out)
}

func count(m hash.Map[string, int], done chan<- int) { done <- m.Len() }

func main() {
	ch := make(chan vec.Vec[int], 2)
	task produce(ch)
	t := 0
	for v := range ch {
		t += v[0]
	}
	m := hash.NewMap[string, int](4)
	m.Set("a", 1)
	done := make(chan int, 1)
	task count(m, done)
	fmt.Println(t, <-done, fmt.Sprint(t))
}