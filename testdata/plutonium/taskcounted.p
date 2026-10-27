package main

// A task statement copies each argument into the new task as an argument
// is assigned to a parameter (spec section 12.12): the task holds one
// reference per counted argument, and it is the parameter's. Overwriting
// the parameter releases it (section 18.7, rule 2), so when the task held
// the last reference, the object is dropped right there, before the task
// goes on. Each task below prints after the assignment; the drop comes
// first whatever the scheduling.

import "pluto/alloc"

type Res owner struct {
	id int
}

func (r *Res) Drop() { println("drop res", r.id) }

func mk(id int) synced[Res] {
	r := alloc.Synced[Res]()
	r.id = id
	return r
}

type Pair struct {
	a synced[Res]
	b synced[Res]
	n int
}

type Holder struct {
	r synced[Res]
}

// A value receiver is a parameter too.
func (h Holder) clear(done chan bool) {
	h = Holder{}
	println("method: receiver cleared")
	done <- true
}

func one(r synced[Res], done chan bool) {
	r = nil
	println("one: cleared")
	done <- true
}

// A struct of several words, overwritten field by field.
func pair(p Pair, done chan bool) {
	p.a = nil
	println("pair: a cleared")
	p = Pair{}
	println("pair: cleared")
	done <- true
}

// A parameter whose address is taken.
func addr(r synced[Res], done chan bool) {
	q := &r
	*q = nil
	println("addr: cleared")
	done <- true
}

func gen[T any](r synced[Res], v T, done chan bool) {
	r = nil
	println("gen: cleared", v)
	done <- true
}

// The function keeps another copy: overwriting the parameter drops
// nothing until the copy goes too.
func kept(r synced[Res], done chan bool) {
	k := r
	r = nil
	println("kept: parameter cleared")
	println("kept: copy holds", k.id)
	k = nil
	println("kept: copy cleared")
	done <- true
}

func main() {
	done := make(chan bool)

	task one(mk(1), done)
	<-done

	task pair(Pair{a: mk(2), b: mk(3), n: 1}, done)
	<-done

	task addr(mk(4), done)
	<-done

	task gen(mk(5), "x", done)
	<-done

	task kept(mk(6), done)
	<-done

	task Holder{r: mk(7)}.clear(done)
	<-done

	task func(r synced[Res], done chan bool) {
		r = nil
		println("literal: cleared")
		done <- true
	}(mk(8), done)
	<-done

	// main keeps its own copy: the task's assignment drops nothing, and
	// main's copy is the last.
	s := mk(9)
	task one(s, done)
	<-done
	println("main still holds", s.id)
	s = nil
	println("end")
}
