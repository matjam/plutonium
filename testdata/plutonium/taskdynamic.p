package main

// A task's counted arguments are its parameters' (spec sections 12.12 and
// 18.7) also when the task calls through an interface method or a func
// value: overwriting the parameter drops the object right there when the
// task held the last reference. Each task below prints after the
// assignment; the drop comes first whatever the scheduling. A function
// that only reads its parameter drops it when it returns.

import "pluto/alloc"

type Res owner struct {
	id int
}

var dropped = make(chan bool, 1)

func (r *Res) Drop() {
	println("drop res", r.id)
	if r.id == 99 {
		dropped <- true
	}
}

func mk(id int) synced[Res] {
	r := alloc.Synced[Res]()
	r.id = id
	return r
}

type Clearer interface {
	clear(r synced[Res], done chan bool)
}

type V struct{ n int }

func (v V) clear(r synced[Res], done chan bool) {
	r = nil
	println("value receiver: cleared")
	done <- true
}

type P struct{ n int }

func (p *P) clear(r synced[Res], done chan bool) {
	r = nil
	println("pointer receiver: cleared")
	done <- true
}

// Wrap has Clearer's method through its embedded field.
type Wrap struct {
	Clearer
}

func one(r synced[Res], done chan bool) {
	r = nil
	println("one: cleared")
	done <- true
}

func gen[T any](r synced[Res], v T, done chan bool) {
	r = nil
	println("gen: cleared", v)
	done <- true
}

// G is generic: its methods are instances.
type G[T any] struct{ v T }

func (g G[T]) clear(r synced[Res], done chan bool) {
	r = nil
	println("generic receiver: cleared", g.v)
	done <- true
}

func look(r synced[Res], done chan bool) {
	println("look: running", r.id)
	done <- true
}

var lit = func(r synced[Res], done chan bool) {
	r = nil
	println("literal: cleared")
	done <- true
}

// Interface values made at package level point at static copies.
var gv V
var gp P
var gc Clearer = V{}
var gw = Wrap{V{}}
var gcw Clearer = Wrap{&gp}
var gg Clearer = G[int]{v: 7}

// Method values made at package level have static records.
var bound = gp.clear
var ibound = gc.clear

func main() {
	done := make(chan bool)

	task gc.clear(mk(1), done)
	<-done

	var cp Clearer = &gp
	task cp.clear(mk(2), done)
	<-done

	task gw.clear(mk(3), done)
	<-done

	task gcw.clear(mk(4), done)
	<-done

	f := one
	task f(mk(5), done)
	<-done

	task lit(mk(6), done)
	<-done

	fg := gen[string]
	task fg(mk(7), "x", done)
	<-done

	mv := V.clear
	task mv(V{}, mk(8), done)
	<-done

	mt := (*V).clear
	task mt(&gv, mk(9), done)
	<-done

	ie := Clearer.clear
	task ie(gc, mk(10), done)
	<-done

	task bound(mk(11), done)
	<-done

	task ibound(mk(12), done)
	<-done

	task gg.clear(mk(14), done)
	<-done

	gm := G[string].clear
	task gm(G[string]{v: "s"}, mk(15), done)
	<-done

	fl := look
	task fl(mk(99), done)
	<-done
	<-dropped
	println("look: dropped after return")

	// main keeps its own copy: the task's assignment drops nothing.
	s := mk(13)
	task gc.clear(s, done)
	<-done
	println("main still holds", s.id)
	s = nil
	println("end")
}
