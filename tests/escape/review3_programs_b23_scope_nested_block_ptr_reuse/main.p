package main

// A pointer to a variable of a nested block, taken by a scoped task whose
// join comes after the block ends (section 19.4: views of storage that ends
// before the join are E3401). A later block's variable may reuse the slot.
import (
	"pluto/sync"
	"pluto/time"
	"unsafe"
)

var got = make(chan int, 1)
var addrs [2]uintptr

//pu:noinline
func f() {
	var s sync.Scope
	{
		y := 42
		p := &y
		addrs[0] = uintptr(unsafe.Pointer(p))
		s.Task(func() {
			time.Sleep(20 * time.Millisecond)
			got <- *p
		})
	}
	{
		z := 7
		addrs[1] = uintptr(unsafe.Pointer(&z))
		for i := 0; i < 1000000; i++ {
			z += i & 1
		}
		println(z)
	}
}

func main() {
	f()
	println("task saw (42 if intact)", <-got, "same slot:", addrs[0] == addrs[1])
}
