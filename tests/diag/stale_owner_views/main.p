package main

import "pluto/vec"

// Views that methods of owners return, of the memory the owner holds on the
// heap, used after the owner moved, was dropped or was given a new value
// (language spec section 18.3). The owners keep that memory in fields that
// only ever hold heap memory, which the escape analysis does not follow
// back to the owner.

// Buf is an owner whose buffer field is heap-only.
type Buf owner struct{ data []int }

func (b *Buf) Slice() []int { return b.data[:len(b.data)] }
func (b *Buf) Drop()        {}

// GBuf is a generic owner.
type GBuf[T any] owner struct{ data []T }

func (b *GBuf[T]) Slice() []T { return b.data[:len(b.data)] }
func (b *GBuf[T]) First() *T  { return &b.data[0] }
func (b *GBuf[T]) Drop()      {}

func takeVec(v vec.Vec[int])  {}
func takeBuf(b Buf)           {}
func takeGBuf(b GBuf[string]) {}

func main() {
	// A vector's slice, after the vector moved by assignment.
	v1 := vec.New[int]()
	v1.Push(1)
	s1 := v1.Slice()
	w1 := v1
	_ = w1
	_ = s1[0]

	// After it moved into a call.
	v2 := vec.New[int]()
	s2 := v2.Slice()
	takeVec(v2)
	_ = s2[0]

	// After an explicit drop.
	v3 := vec.New[int]()
	s3 := v3.Slice()
	v3.Drop()
	_ = s3[0]

	// A pointer to an element, after the vector is given a new value.
	v4 := vec.New[int]()
	v4.Push(4)
	p4 := v4.Ptr(0)
	v4 = vec.New[int]()
	_ = *p4

	// A user owner's slice, after the owner moved into a call.
	b6 := Buf{}
	s6 := b6.Slice()
	takeBuf(b6)
	_ = s6[0]

	// A user generic owner's slice, after the owner moved by assignment.
	b7 := GBuf[int]{}
	s7 := b7.Slice()
	w7 := b7
	_ = w7
	_ = s7[0]

	// A pointer a user generic owner returns, after it moved into a call.
	b8 := GBuf[string]{}
	p8 := b8.First()
	takeGBuf(b8)
	_ = *p8

	// Fine: a view used before the move, a length used after, and a view
	// of the owner it moved to.
	v9 := vec.New[int]()
	s9 := v9.Slice()
	_ = s9
	n9 := v9.Len()
	w9 := v9
	t9 := w9.Slice()
	_ = n9
	_ = t9
}
