package main

// A struct may embed shared[T] or synced[T] (spec sections 7.6 and 7.19):
// T's methods are promoted as through an embedded *T, the field is named T,
// and the struct copies, counting each copy. T is dropped once, when the
// last copy goes.

import "pluto/alloc"

type Res owner struct {
	id   int
	size int
	bold bool
}

func (r *Res) Drop() { println("drop res", r.id) }

func (r *Res) Bold() bool { return r.bold }

func (r *Res) Grow() { r.size++ }

type Widget struct {
	shared[Res]
	name string
}

type SWidget struct {
	synced[Res]
	name string
}

type Holder struct {
	Widget
	n int
}

type Bolder interface{ Bold() bool }

func mk(id int) Widget {
	w := Widget{Res: alloc.Shared[Res](), name: "w"}
	w.id = id
	w.bold = true
	return w
}

// use gets a counted copy, which it drops when it returns.
func use(w Widget) int {
	w.Grow()
	return w.size
}

func work(s SWidget, done chan int) {
	s.Grow()
	done <- s.size
}

func main() {
	w := mk(1)
	println(w.Bold(), w.size, w.name)
	w.Grow()
	{
		w2 := w // a counted copy of the same Res
		w2.Grow()
		n := use(w2)
		println(w.size, w2.size, n)
	}
	println("after the copy's block", w.size)

	h := Holder{Widget: w, n: 1}
	h.Grow()
	println(h.size, h.Bold(), w.size)

	var b Bolder = w // the promoted method satisfies the interface
	println(b.Bold())

	// A synced box may cross to another task.
	s := SWidget{Res: alloc.Synced[Res](), name: "s"}
	s.id = 2
	done := make(chan int)
	task work(s, done)
	println("task saw", <-done)
	s2 := s
	s2.Grow()
	println(s.size, s2.size)
	println("end")
}
