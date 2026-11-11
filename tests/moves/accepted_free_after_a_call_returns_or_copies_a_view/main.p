package main

import (
	"pluto/alloc"
	"pluto/bytes"
	"pluto/vec"
)

// A call that only returns its argument does not keep it, and a result
// that holds only bytes copied from a view holds no view (language spec
// section 18.3). An error interface counts a synced reference rather than
// lending it (sections 7.9 and 16.7).

func first(s []int) []int { return s[:1] }

type E owner struct{ n int }

func (e *E) Error() string { return "e" }

func main() {
	v := vec.New[int]()
	v.Push(4)
	println(first(v.Slice())[0])
	v.Free()

	data := vec.New[byte]()
	data.Push('x')
	rf := bytes.NewBuffer(data.Slice())
	var h bytes.Buffer
	h.ReadFrom(&rf)
	data.Free()
	println(h.Len(), rf.Len())
	h.Free()
	rf.Free()

	p := alloc.Synced[E]()
	p.n = 5
	var e error = p
	p = nil
	println(e.Error())
}
