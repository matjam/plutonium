package main

// Many calls that may end with rt.ExitTask, made with different owners
// alive and different deferred calls armed. Each exit runs the deferred
// calls armed at that point, last first, then drops the owners alive at
// that point: inner scopes first, each in reverse order of declaration,
// then the parameters. An owner known to hold nothing is not dropped
// (language spec sections 17.3 and 18.6). The exits share their cleanup
// code, so each must still release only what is alive where it leaves.

import "pluto/rt"

type H owner struct{ id int }

func (h *H) Drop() {
	if h.id != 0 {
		println("drop", h.id)
	}
}

type Pair owner struct{ x, y H }

var stop int

func step(i int) {
	if i == stop {
		rt.ExitTask()
	}
}

func take(h H) { println("took", h.id) }

func many() {
	defer println("deferred first")
	a := H{id: 1}
	_ = a.id
	step(1)
	b := H{id: 2}
	_ = b.id
	step(2)
	defer println("deferred second")
	{
		c := H{id: 3}
		_ = c.id
		step(3)
		d := H{id: 4}
		_ = d.id
		step(4)
	}
	step(5)
	take(b)
	step(6)
	e := Pair{y: H{id: 5}}
	_ = e.y.id
	for i := 0; i < 2; i++ {
		f := H{id: 10 + i}
		_ = f.id
		step(7 + i)
	}
	step(9)
	println("finished")
}

func withParam(p H) {
	defer println("param deferred")
	_ = p.id
	r := H{id: 31}
	_ = r.id
	step(11)
	s := H{id: 32}
	_ = s.id
	step(12)
	println("withParam finished")
}

func main() {
	for i := 1; i <= 10; i++ {
		stop = i
		println("stop", i)
		println(rt.CatchExitTask(many))
	}
	for i := 11; i <= 13; i++ {
		stop = i
		println("stop", i)
		println(rt.CatchExitTask(func() { withParam(H{id: 30}) }))
	}
}
