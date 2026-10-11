package main

// len of a ring calls its Len method: the number of elements it holds
// (language spec sections 13.6 and 7.17). The operand is an addressable
// ring or a pointer to one, since Len has a pointer receiver.

type Q ring[int, 8]

type Holder struct {
	n int
	r ring[byte, 2]
}

func size(p *ring[string, 4]) int { return len(p) }

func main() {
	var r ring[int, 4]
	println(len(r))
	r.Push(1)
	r.Push(2)
	println(len(r), r.Len())
	r.Pop()
	println(len(r), len(&r))

	var s ring[string, 4]
	s.Push("a")
	println(size(&s))

	var q Q
	q.Push(3)
	println(len(q))

	var h Holder
	h.r.Push('x')
	h.r.Push('y')
	p := &h
	println(len(p.r), len(h.r) == h.r.Cap())

	// A full ring and an emptied one.
	for i := 0; i < 8; i++ {
		r.Push(i)
	}
	println(len(r), r.Full())
	r.Reset()
	println(len(r), r.Empty())
}
