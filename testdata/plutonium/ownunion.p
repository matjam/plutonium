package main

// Owner unions (language spec section 5.10): a union whose variants hold
// owners is declared owner union. It drops the payload of the variant it
// holds when it is dropped or given another variant, it moves instead of
// being copied, and a type switch with a binding moves the payload into the
// binding. Unions may also hold counted references (shared, synced), which
// copy and count. Every Drop prints; the program also runs under the debug
// allocator, which fails on a leak.

import "pluto/alloc"

type File owner struct{ name string }

func (f *File) Drop() {
	if f.name != "" { // the zero value, left by a move, holds nothing
		println("close", f.name)
	}
}

type Node owner struct{ name string }

func (n *Node) Drop() { println("drop node", n.name) }

type Slot owner union {
	Empty
	Open  File
	Boxed owned[Node]
	Code  int
}

// Ref holds a counted reference: it copies, and counts.
type Ref union {
	None
	Some shared[Node]
}

func open(name string) Slot {
	return Slot.Open(File{name: name})
}

func boxed(name string) Slot {
	n := new(Node)
	n.name = name
	return Slot.Boxed(n) // n moves into the union
}

func describe(s Slot) string {
	switch v := s.(type) {
	case Empty:
		return "empty"
	case Open:
		return "file " + v.name
	case Boxed:
		return "node " + v.name
	case Code:
		if v > 0 {
			return "code"
		}
		return "zero code"
	}
	return ""
} // the payload moved into the clause's v is dropped there

func main() {
	a := open("a")
	println(describe(a)) // a moves into describe
	b := boxed("b")
	b = open("c") // drops the node b held
	b = Slot.Code(3)
	println(describe(b))
	{
		c := boxed("scoped")
		_ = c
	} // c's node is dropped here

	d := open("d")
	if f, ok := d.(Slot.Open); ok {
		println("took", f.name)
	} // f is dropped here; d was cleared

	e := open("e")
	if _, ok := e.(Slot.Boxed); ok {
		println("not reached")
	} else {
		println("still a file:", describe(e))
	}

	n := alloc.Shared[Node]()
	n.name = "shared"
	r := Ref.Some(n)
	r2 := r // a copy: one more reference
	switch v := r2.(type) {
	case Some:
		println("ref", v.name)
	case None:
	}
	r = Ref.None
	println("end", n.name)
}
