package main

// A function whose body ends in a panic never returns by the compiler's
// inference, but rt.ExitTask before the panic ends the task: the exit
// leaves each frame through its return path, so the call returns to its
// caller, which must run its deferred calls and drop its owners before it
// leaves in turn (language spec section 17.3).

import "pluto/rt"

type H owner struct{ id int }

func (h *H) Drop() {
	if h.id != 0 {
		println("drop", h.id)
	}
}

func fail() {
	rt.ExitTask()
	panic("not reached: fail")
}

// failAgain never returns either: it ends in a call of fail.
func failAgain() {
	h := H{id: 2}
	_ = h.id
	fail()
}

func failInt(n int) int {
	if n > 0 {
		rt.ExitTask()
	}
	panic("not reached: failInt")
}

func direct() {
	defer println("direct deferred")
	h := H{id: 1}
	_ = h.id
	fail()
	println("not reached: direct")
}

func transitive() {
	defer println("transitive deferred")
	h := H{id: 3}
	_ = h.id
	failAgain()
	println("not reached: transitive")
}

func result() {
	h := H{id: 4}
	_ = h.id
	n := failInt(1)
	println("not reached: result", n)
}

func main() {
	println(rt.CatchExitTask(direct))
	println(rt.CatchExitTask(transitive))
	println(rt.CatchExitTask(result))
	println("end")
}
