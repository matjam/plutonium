package main
import (
	"pluto/sync"
	"pluto/vec"
)

type pair struct{ a, b int }

var (
	mu    sync.Mutex
	ring1 ring[int, 8]
	items vec.Vec[int]
	arr   [4]pair
	buf   [16]byte
	total int
	label string
)

func seq(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func two() (int, string) { return 2, "two" }

func (p *pair) sum() int { return p.a + p.b }

func work(id int, done chan bool) {
	mu.Lock()
	defer mu.Unlock()
	for v := range seq(3) {
		total += v
	}
	ring1.Push(id)
	items.Push(id)
	items[0] = id
	n, s := two()
	label = s
	arr[id%4] = pair{n, id}
	local := pair{1, 2}
	f := local.sum // a method value of a local: called later, with no lock
	total += f()
	copy(buf[:], "hello")
	switch {
	case id == 0:
		total++
		fallthrough
	case id == 1:
		total += 2
	default:
	}
	select {
	case done <- true:
	default:
		task func(done chan bool) { done <- true }(done)
	}
outer:
	for i := range arr {
		for j := range arr {
			if i+j > 3 {
				break outer
			}
			if j > i {
				continue outer
			}
			total += arr[i].a
		}
	}
	var x any = arr[1]
	if p, ok := x.(pair); ok {
		total += p.b
	}
	switch v := x.(type) {
	case pair:
		total += v.a
	}
	_ = take(&label)
}

func main() {
	done := make(chan bool)
	task work(0, done)
	task work(1, done)
	<-done
	<-done
	mu.Lock()
	println(total, label, len(items))
	mu.Unlock()
}