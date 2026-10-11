// Generic code over Plutonium's own types: packed types as type arguments
// and in generic structs, packed conversions through type sets, volatile
// and aligned values, rings of a type parameter, allocation with new and
// vec.Make in generic functions, and the pluto/alloc generics.
package main

import (
	"pluto/alloc"
	"pluto/vec"
	"unsafe"
)

type Ctrl packed uint32 {
	mode   bits[uint, 4]
	enable bits[bool, 1]
	prio   bits[uint, 3]
	_      bits[uint, 24]
}

type Flags packed uint8 {
	f bits {
		A
		B
		C
	}
	n bits[uint8, 5]
}

type Hdr packed be [4]byte {
	kind bits[uint8, 4]
	ver  bits[uint8, 4]
	len  uint16
	_    uint8
}

// A packed type converts to and from its backing integer through a type
// set of packed types.
type Reg32 interface{ ~uint32 }

func Raw[P interface{ Ctrl }](p P) uint32 { return uint32(p) }

func FromRaw[P interface{ Ctrl }](v uint32) P { return P(v) }

func Same[T comparable](a, b T) bool { return a == b }

func Swap[T any](a, b *T) { *a, *b = *b, *a }

type Slot[T any] struct {
	val   T
	valid bool
}

func (s *Slot[T]) Set(v T) { s.val, s.valid = v, true }

func (s Slot[T]) Get() (T, bool) { return s.val, s.valid }

func Size[T any]() uintptr {
	var z T
	return unsafe.Sizeof(z)
}

func Fill[T any](xs []T, v T) {
	for i := range xs {
		xs[i] = v
	}
}

// Volatile and aligned storage of a type parameter's type.
type Dev[T ~uint8 | ~uint32] struct {
	status volatile[T]
	data   volatile[T]
}

func (d *Dev[T]) Write(v T) T {
	d.data = v
	d.status = d.status | 1
	return d.data + d.status
}

type Cache[T any] struct {
	line aligned[T, 64]
	n    int
}

func Put[T any](c *Cache[T], v T) { c.line = v; c.n++ }

// Rings of a type parameter.
func Drain[T any](r *ring[T, 4], f func(T)) int {
	n := 0
	for {
		v, ok := r.Pop()
		if !ok {
			return n
		}
		f(v)
		n++
	}
}

func Fill4[T any](r *ring[T, 4], v T) int {
	n := 0
	for r.Push(v) {
		n++
	}
	return n
}

// Allocation in generic code.
func NewOf[T any](v T) owned[T] { return new(v) }

func MakeN[T any](n int, v T) vec.Vec[T] {
	s := vec.Make[T](n)
	Fill(s.Slice(), v)
	return s
}

type Node[T any] struct {
	v    T
	next *Node[T]
}

func Build[T any](vs ...T) *Node[T] {
	var head *Node[T]
	for i := len(vs) - 1; i >= 0; i-- {
		n := alloc.New[Node[T]]()
		n.v, n.next = vs[i], head
		head = n
	}
	return head
}

func FreeAll[T any](n *Node[T]) int {
	c := 0
	for n != nil {
		next := n.next
		alloc.Free(n)
		n = next
		c++
	}
	return c
}

func main() {
	var c Ctrl
	c.mode = 9
	c.enable = true
	c.prio = 5
	println(Raw(c), FromRaw[Ctrl](0x1f).mode, FromRaw[Ctrl](0x1f).enable)
	d := c
	d.prio = 2
	println(Same(c, c), Same(c, d), Size[Ctrl](), Size[Flags](), Size[Hdr]())
	Swap(&c, &d)
	println(c.prio, d.prio)

	var f Flags
	f.f.B = true
	f.n = 31
	var fs Slot[Flags]
	fs.Set(f)
	g, ok := fs.Get()
	println(g.f.A, g.f.B, g.n, ok, uint8(g))

	h := Hdr{kind: 3, ver: 4, len: 0x0102}
	hs := MakeN(3, h)
	println(hs.Len(), hs[2].kind, hs[2].ver, hs[2].len, Same(hs[0], h))
	hs.Free()

	var dv Dev[uint8]
	println(dv.Write(4), dv.status)
	var c32 Cache[uint32]
	Put(&c32, 77)
	println(c32.line, c32.n, Size[Cache[uint32]](), uintptr(unsafe.Pointer(&c32.line))%64)

	var r ring[string, 4]
	println(Fill4(&r, "s"), r.Len())
	println(Drain(&r, func(s string) { print(s) }), r.Empty())
	var rc ring[Ctrl, 4]
	rc.Push(c)
	println(Drain(&rc, func(v Ctrl) { print(v.mode, " ") }))

	p := NewOf(Slot[int]{val: 5})
	println(p.val, *NewOf(3), *NewOf("q"))
	l := Build(1, 2, 3)
	// FreeAll is called before the operands are read (spec section
	// 11.26), so the list is read in a statement of its own.
	println(l.v, l.next.v, l.next.next.v)
	println(FreeAll(l))
}
