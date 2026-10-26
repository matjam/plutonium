package main

// Claim 4b: a type-assertion filter that survives a path cut at the depth
// limit. The method keeps a pointer four loads below its receiver; the frame
// nodes are of a type (Node) different from what the filter names, and the
// chain is reached through a type assertion to Keeper.
import "unsafe"

type Keeper interface{ Keep() }

type Node struct {
	next *Node
	n    int
}

var keep *Node

func (n *Node) Keep() { keep = n.next.next.next.next }

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
	var n4 Node
	n4.n = 4
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
