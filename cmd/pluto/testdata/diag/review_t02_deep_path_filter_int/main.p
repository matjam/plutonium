package main

// As t01, but what is kept is a *int (a type that does not implement Keeper)
// found past the depth limit, so a filter wrongly applied to everything
// reachable would drop it.
import "unsafe"

type Keeper interface{ Keep() }

type Node struct {
	next *Node
	p    *int
}

var keep *int

func (n *Node) Keep() { keep = n.next.next.next.next.p }

type Wide struct{ a, b, c int }

func (w Wide) Keep() {}

//pu:noinline
func call(v any) {
	if k, ok := v.(Keeper); ok {
		k.Keep()
	}
}

//pu:noinline
func f() {
	x := 99
	n4 := Node{p: &x}
	n3 := Node{next: &n4}
	n2 := Node{next: &n3}
	n1 := Node{next: &n2}
	n0 := Node{next: &n1}
	call(&n0)
	call(Wide{1, 2, 3})
}

func main() {
	var mark int
	f()
	if keep == nil {
		println("method not called")
		return
	}
	println("dangling:", uintptr(unsafe.Pointer(keep)) < uintptr(unsafe.Pointer(&mark)))
}
