package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/vec"
)

// Views of the memory a local owner or a local arena holds follow the frame
// rules (spec 18.3, "A local owner's memory"; 18.9): they may not be
// returned or stored where they outlive the owner.

var keepInts []int
var keepInt *int

func sliceOfLocal() []int {
	v := vec.New[int]()
	v.Push(1)
	return v.Slice()
}

func ptrOfLocal() *int {
	v := vec.New[int]()
	v.Push(1)
	return v.Ptr(0)
}

func storeLocal() {
	v := vec.New[int]()
	v.Push(1)
	keepInts = v.Slice()
}

type Bag owner struct{ items vec.Vec[int] }

func (b *Bag) Items() []int { return b.items.Slice() }

func throughMethod() []int {
	var b Bag
	b.items = vec.New[int]()
	b.items.Push(1)
	return b.Items()
}

type node struct{ v int }

type Box owner struct{ n *node }

func (b *Box) Node() *node { return b.n }
func (b *Box) Drop()       { alloc.Free(b.n) }

func ownedPointer() *node {
	b := Box{n: alloc.New[node]()}
	return b.Node()
}

func fromFixed() *int {
	a := arena.NewFixed(64)
	return a.New[int]()
}

func storeChunked() {
	a := arena.NewChunked()
	keepInt = a.New[int]()
}

// Fine: the owner comes from the caller, so the view is the caller's.
func sliceOfParam(v *vec.Vec[int]) []int { return v.Slice() }

// Fine: views used and passed down while the owner lives.
func sum(s []int) int {
	t := 0
	for _, x := range s {
		t += x
	}
	return t
}

func first(s []int) []int { return s[:1] }

func local() int {
	v := vec.New[int]()
	v.Push(1)
	s := first(v.Slice())
	a := arena.NewChunked()
	p := a.New[int]()
	*p = s[0]
	return sum(v.Slice()) + *p + v.Len()
}

// Fine: views of an owner's memory stored into that memory.
type Names owner struct {
	bytes vec.Vec[byte]
	ents  vec.Vec[[]byte]
}

func names() Names {
	n := Names{bytes: vec.New[byte](), ents: vec.New[[]byte]()}
	n.bytes.Append('a', 'b')
	n.ents.Push(nil)
	all := n.bytes.Slice()
	ents := n.ents.Slice()
	ents[0] = all[:1]
	return n
}

// Fine: the owner itself is returned.
func whole() vec.Vec[int] {
	v := vec.New[int]()
	v.Push(1)
	return v
}

func main() {
	println(sliceOfLocal()[0], *ptrOfLocal(), throughMethod()[0], ownedPointer().v, *fromFixed())
	storeLocal()
	storeChunked()
	v := whole()
	n := names()
	println(sliceOfParam(&v)[0], local(), keepInts[0], *keepInt, n.ents.Len())
}
