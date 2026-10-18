package race_test

import (
	"strings"
	"testing"
)

// Stores to a per-CPU variable that depend on an earlier load of the same
// variable (language spec section 20.9, decision D37).

// Programs that must be accepted.
var perCPUDepAccepted = []struct{ name, src string }{
	{"two independent lone accesses", `
type Thread struct{ id int }

//pu:percpu
var current *Thread

func log(t *Thread) { println(t == nil) }

func main() {
	log(current)
	println("between")
	u := &Thread{id: 3}
	current = u
}`},
	{"a value that was not computed from the load", `
//pu:percpu
var ticks uint64

func main() {
	x := ticks
	println(x)
	ticks = 7
}`},
	{"a local overwritten before the store", `
//pu:percpu
var ticks uint64

func main() {
	x := ticks
	x = 5
	ticks = x
}`},
	{"a store that depends on another variable", `
//pu:percpu
var a uint64

//pu:percpu
var b uint64

func main() {
	x := a
	b = x + 1
}`},
	{"a store that depends on a load of an ordinary variable", `
var n uint64

//pu:percpu
var ticks uint64

func main() {
	x := n
	ticks = x + 1
}`},
	{"load and store in one region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	x := ticks
	ticks = x + 1
	rt.EnablePreemption()
}`},
	{"load and store in one region with defer", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func count() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	x := ticks
	y := x * 2
	ticks = y + 1
}

func main() { count() }`},
	{"a condition in one region", `
import "pluto/rt"

type Thread struct{ id int }

//pu:percpu
var current *Thread

func main() {
	idle, next := &Thread{id: 0}, &Thread{id: 1}
	rt.DisablePreemption()
	if current == idle {
		current = next
	}
	rt.EnablePreemption()
}`},
	{"the region is the caller's", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func bump() {
	x := ticks
	ticks = x + 1
}

func main() {
	rt.DisablePreemption()
	bump()
	rt.EnablePreemption()
}`},
	{"the region is held across a loop", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	for i := 0; i < 3; i++ {
		x := ticks
		ticks = x + 1
	}
	rt.EnablePreemption()
}`},
	{"a region in each iteration", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	for i := 0; i < 3; i++ {
		rt.DisablePreemption()
		x := ticks
		ticks = x + 1
		rt.EnablePreemption()
	}
}`},
	{"interrupts disabled with Cli and Sti", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func main() {
	x86.Cli()
	x := ticks
	ticks = x + 1
	x86.Sti()
}`},
	{"an rt.Lock held", `
import "pluto/rt"

//pu:percpu
var ticks uint64

var l rt.Lock

func main() {
	l.Lock()
	x := ticks
	ticks = x + 1
	l.Unlock()
}`},
	{"a call of a function that loads, in the region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func peek() uint64 { return ticks }

func main() {
	rt.DisablePreemption()
	x := peek()
	ticks = x + 1
	rt.EnablePreemption()
}`},
	{"a call of a function that stores from its parameter, in the region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func set(v uint64) { ticks = v }

func main() {
	rt.DisablePreemption()
	set(ticks + 1)
	rt.EnablePreemption()
}`},
	{"a store controlled by another variable's load", `
//pu:percpu
var a uint64

//pu:percpu
var b uint64

func main() {
	if a == 0 {
		b = 1
	}
}`},
	{"a call of a function that stores, under an unrelated condition", `
//pu:percpu
var ticks uint64

func reset() { ticks = 0 }

func main() {
	n := 3
	if n > 2 {
		reset()
	}
}`},
	{"a handler's body is a region", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x := ticks
	ticks = x + 1
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }`},
}

func TestPerCPUDepAccepted(t *testing.T) {
	for _, c := range perCPUDepAccepted {
		t.Run(c.name, func(t *testing.T) {
			if err := checkKernel(t, c.src); err != nil {
				t.Fatalf("rejected:\n%s", fullText(err))
			}
		})
	}
}

// Programs that must be rejected: the substrings of the output, and the
// number of errors.
var perCPUDepRejected = []struct {
	name, src string
	want      []string
	n         int
}{
	{"a load, then a store of a value computed from it", `
//pu:percpu
var ticks uint64

func main() {
	x := ticks
	ticks = x + 1
}`, []string{"main.p:7:2: ticks = ... uses the per-CPU variable ticks outside a region where the task cannot move to another processor", "a store of a value that depends on the load of ticks at main.p:6", "as long as the store does not depend on a load of the same variable", "E2022"}, 1},
	{"a store controlled by a load", `
type Thread struct{ id int }

//pu:percpu
var current *Thread

func main() {
	idle, next := &Thread{id: 0}, &Thread{id: 1}
	if current == idle {
		current = next
	}
}`, []string{"main.p:10:3: current = ... uses the per-CPU variable current outside a region", "a store controlled by a condition that depends on the load of current at main.p:9"}, 1},
	{"a value through a struct copy", `
type S struct{ n, m uint64 }

//pu:percpu
var ticks uint64

func main() {
	s := S{n: ticks}
	t := s
	ticks = t.n + 1
}`, []string{"main.p:10:2: ticks = ... uses the per-CPU variable ticks outside a region", "depends on the load of ticks at main.p:8"}, 1},
	{"a value through several locals and arithmetic", `
//pu:percpu
var ticks uint64

func main() {
	a := ticks
	b := a*2 + 3
	c := b >> 1
	ticks = c
}`, []string{"main.p:9:2: ticks = ... uses the per-CPU variable ticks outside a region"}, 1},
	{"a value through an array element of a local", `
//pu:percpu
var ticks uint64

func main() {
	var arr [4]uint64
	arr[2] = ticks
	ticks = arr[1]
}`, []string{"main.p:8:2: ticks = ... uses the per-CPU variable ticks outside a region"}, 1},
	{"a value through a visible call", `
//pu:percpu
var ticks uint64

func inc(v uint64) uint64 { return v + 1 }

func main() {
	x := ticks
	ticks = inc(x)
}`, []string{"main.p:9:2: ticks = ... uses the per-CPU variable ticks outside a region"}, 1},
	{"a value through a call that cannot be seen", `
//pu:percpu
var ticks uint64

func main() {
	f := func(v uint64) uint64 { return v }
	var g func(uint64) uint64 = f
	x := ticks
	y := g(x)
	ticks = y
}`, []string{"main.p:10:2: ticks = ... uses the per-CPU variable ticks outside a region", "The dependence passes through a call of g, whose body the analysis cannot see"}, 1},
	{"a value through a call that cannot be seen, in a loop", `
//pu:percpu
var ticks uint64

//pu:extern ext
func ext(v uint64) uint64

func main() {
	for i := 0; i < 2; i++ {
		ticks = ext(ticks)
	}
}`, []string{"uses the per-CPU variable ticks outside a region"}, 1},
	{"a function that loads the variable", `
//pu:percpu
var ticks uint64

func peek() uint64 { return ticks }

func main() {
	x := peek()
	ticks = x + 1
}`, []string{"main.p:9:2: ticks = ... uses the per-CPU variable ticks outside a region", "the call of peek at main.p:8, which loads ticks"}, 1},
	{"a function that loads the variable through another", `
//pu:percpu
var ticks uint64

func peek() uint64 { return ticks }

func twice() uint64 { return peek() * 2 }

func main() {
	x := twice()
	ticks = x
}`, []string{"main.p:11:2: ticks = ... uses the per-CPU variable ticks outside a region", "which loads ticks"}, 1},
	{"a get then a set of the same value", `
type Thread struct{ id int }

//pu:percpu
var current *Thread

func get() *Thread { return current }

func set(t *Thread) { current = t }

func main() {
	t := get()
	set(t)
}`, []string{"main.p:13:2: the call of set uses the per-CPU variable current outside a region", "a call of set, which stores current, from a value or under a condition that depends on the call of get at main.p:12"}, 1},
	{"a callee that stores from its parameter", `
//pu:percpu
var ticks uint64

func set(v uint64) { ticks = v }

func main() {
	set(ticks + 1)
}`, []string{"main.p:8:2: the call of set uses the per-CPU variable ticks outside a region", "a call of set, which stores ticks"}, 1},
	{"a callee that stores under the caller's condition", `
type Thread struct{ id int }

//pu:percpu
var current *Thread

var next = &Thread{id: 1}

func switchTo() { current = next }

func main() {
	if current == nil {
		switchTo()
	}
}`, []string{"main.p:13:3: the call of switchTo uses the per-CPU variable current outside a region", "which stores current"}, 1},
	{"a callee that stores from a parameter through another callee", `
//pu:percpu
var ticks uint64

func set(v uint64) { ticks = v }

func setTwice(v uint64) { set(v * 2) }

func main() {
	x := ticks
	setTwice(x)
}`, []string{"the call of setTwice uses the per-CPU variable ticks outside a region"}, 1},
	{"a method call that stores from its receiver", `
//pu:percpu
var ticks uint64

type C struct{ n uint64 }

func (c C) save() { ticks = c.n }

func main() {
	c := C{n: ticks}
	c.save()
}`, []string{"main.p:11:2: the call of C.save uses the per-CPU variable ticks outside a region"}, 1},
	{"a value from a func value that loads the variable", `
//pu:percpu
var ticks uint64

func main() {
	get := func() uint64 { return ticks }
	x := get()
	ticks = x + 1
}`, []string{"main.p:8:2: ticks = ... uses the per-CPU variable ticks outside a region", "depends on the load of ticks at main.p:6"}, 1},
	{"a value from a function that runs a literal that loads", `
//pu:percpu
var ticks uint64

func apply(f func() uint64) uint64 { return f() }

func main() {
	x := apply(func() uint64 { return ticks })
	ticks = x + 1
}`, []string{"main.p:9:2: ticks = ... uses the per-CPU variable ticks outside a region"}, 1},
	{"a func value that stores from its argument", `
//pu:percpu
var ticks uint64

func main() {
	set := func(v uint64) { ticks = v }
	set(ticks + 1)
}`, []string{"main.p:7:2: the call of set uses the per-CPU variable ticks outside a region"}, 1},
	{"an early return controlled by the load", `
type Thread struct{ id int }

//pu:percpu
var current *Thread

func main() {
	idle, next := &Thread{id: 0}, &Thread{id: 1}
	if current != idle {
		return
	}
	current = next
}`, []string{"main.p:12:2: current = ... uses the per-CPU variable current outside a region", "a store controlled by a condition that depends on the load of current at main.p:9"}, 1},
	{"a break controlled by the load", `
//pu:percpu
var ticks uint64

func main() {
	for i := 0; i < 5; i++ {
		if ticks > 3 {
			break
		}
	}
	ticks = 0
}`, []string{"uses the per-CPU variable ticks outside a region", "controlled by a condition"}, 1},
	{"a switch on the load", `
type Thread struct{ id int }

//pu:percpu
var current *Thread

func main() {
	idle, next := &Thread{id: 0}, &Thread{id: 1}
	switch current {
	case idle:
		current = next
	}
}`, []string{"main.p:11:3: current = ... uses the per-CPU variable current outside a region"}, 1},
	{"a store in a loop from the load of the previous iteration", `
//pu:percpu
var ticks uint64

func main() {
	var x uint64
	for i := 0; i < 3; i++ {
		if i > 0 {
			ticks = x
		}
		x = ticks
	}
}`, []string{"main.p:9:4: ticks = ... uses the per-CPU variable ticks outside a region"}, 1},
	{"a store from a package variable that was set from the load", `
var saved uint64

//pu:percpu
var ticks uint64

func main() {
	saved = ticks
	ticks = saved + 1
}`, []string{"main.p:9:2: ticks = ... uses the per-CPU variable ticks outside a region"}, 1},
	{"a value through a pointer to a local", `
//pu:percpu
var ticks uint64

func main() {
	var x uint64
	p := &x
	*p = ticks
	ticks = x
}`, []string{"main.p:9:2: ticks = ... uses the per-CPU variable ticks outside a region"}, 1},
	{"the load and the store in two regions", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	x := ticks
	rt.EnablePreemption()
	rt.DisablePreemption()
	ticks = x + 1
	rt.EnablePreemption()
}`, []string{"main.p:12:2: ticks = ... uses the per-CPU variable ticks in a region that does not also hold the load it depends on", "The load and the store must be inside the same region"}, 1},
	{"the load outside the region of the store", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	x := ticks
	rt.DisablePreemption()
	ticks = x + 1
	rt.EnablePreemption()
}`, []string{"main.p:10:2: ticks = ... uses the per-CPU variable ticks in a region that does not also hold the load it depends on"}, 1},
	{"the same two regions in a loop", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var x uint64
	for i := 0; i < 3; i++ {
		rt.DisablePreemption()
		if i > 0 {
			ticks = x
		}
		x = ticks
		rt.EnablePreemption()
	}
}`, []string{"main.p:12:4: ticks = ... uses the per-CPU variable ticks in a region that does not also hold the load it depends on"}, 1},
	{"a callee that releases the caller's region", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func reopen() {
	rt.EnablePreemption()
	rt.DisablePreemption()
}

func main() {
	rt.DisablePreemption()
	x := ticks
	reopen()
	ticks = x + 1
	rt.EnablePreemption()
}`, []string{"main.p:16:2: ticks = ... uses the per-CPU variable ticks in a region that does not also hold the load it depends on"}, 1},
	{"a literal that stores from an outside load", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func within(f func()) {
	rt.DisablePreemption()
	f()
	rt.EnablePreemption()
}

func main() {
	x := ticks
	within(func() { ticks = x + 1 })
}`, []string{"ticks = ... uses the per-CPU variable ticks"}, 1},
	{"a store depending on a load in a handler", `
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	x := ticks
	ticks = x + 1
}`, []string{"main.p:15:2: ticks = ... uses the per-CPU variable ticks outside a region with interrupts disabled, which it needs because the interrupt handler timer also uses it"}, 1},
}

func TestPerCPUDepRejected(t *testing.T) {
	for _, c := range perCPUDepRejected {
		t.Run(c.name, func(t *testing.T) {
			err := checkKernel(t, c.src)
			if err == nil {
				t.Fatal("accepted")
			}
			out := fullText(err)
			got := strings.Join(strings.Fields(out), " ")
			for _, w := range c.want {
				if !strings.Contains(got, strings.Join(strings.Fields(w), " ")) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
			if n := strings.Count(out, `pluto explain E2022`); c.n >= 0 && n != c.n {
				t.Errorf("%d E2022 errors, want %d:\n%s", n, c.n, out)
			}
		})
	}
}
