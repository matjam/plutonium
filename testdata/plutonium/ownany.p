package main

// An owning interface with no methods, type Any owner interface{}, owns
// whatever is converted into it and drops it when it is dropped, whatever
// its dynamic type (language spec section 4.4). With no methods it has no
// method table, so the drop code is a word of the type descriptor. Every
// Drop prints, so the output shows when each value goes; the program also
// runs under the debug allocator, which fails on a leak.

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync"
)

type Any owner interface{}

type File owner struct{ name string }

func (f *File) Drop() { println("close", f.name) }

// Big is larger than a word: in an owning interface it is held in the
// frame of the function that converts it.
type Big owner struct {
	name string
	pad  [4]int
}

func (b *Big) Drop() {
	if b.name != "" { // a moved-from Big is the zero value, and holds nothing
		println("drop big", b.name)
	}
}

type Fd owner int32

func (f *Fd) Drop() { println("drop fd", int32(*f)) }

type Node owner struct{ n string }

func (n *Node) Drop() { println("drop node", n.n) }

// Named has a method, so a value can also be held by an owning interface
// with methods, and moved from there into an Any.
type Named owner interface{ Name() string }

func (f *File) Name() string { return f.name }

type Plain struct{ s string }

func file(name string) owned[File] {
	f := new(File)
	f.name = name
	return f
}

// scope drops its interfaces at the end of the blocks that hold them.
func scope() {
	var a Any = file("scope-a")
	{
		var b Any = new(Fd(3))
		var c Any = Plain{"plain"} // nothing to drop
		var d Any = 42
		_, _, _ = b, c, d
		println("inner end")
	}
	println("outer end", a != nil)
}

// early returns from the middle: what is held so far is dropped.
func early(n int) string {
	var a Any = file("early-a")
	_ = a
	if n == 0 {
		return "first"
	}
	var b Any = new(Node{n: "early-b"})
	_ = b
	return "second"
}

// reassign drops the old value when the variable gets a new one.
func reassign() {
	var a Any = file("old")
	a = new(Fd(1)) // drops old
	a = nil        // drops fd
	println("reassigned", a == nil)
	a = file("last")
}

func take(a Any) {
	println("taking")
} // a, the parameter, is dropped here

func pass() Any { return file("passed") }

// counted holds shared and synced references, released when the interface is.
func counted() {
	s := alloc.Shared[Node]()
	s.n = "shared"
	y := alloc.Synced[Node]()
	y.n = "synced"
	{
		var a Any = s
		var b Any = y
		_, _ = a, b
		println("holding")
	}
	println("still", s.n, y.n)
}

// widen moves an owning interface with methods into one without.
func widen() {
	var n Named = file("named")
	println(n.Name())
	var a Any = n // moves
	_ = a
	println("widened")
}

// asserted moves the owner out of the interface.
func asserted() {
	var a Any = file("asserted")
	if f, ok := a.(owned[File]); ok {
		println("got", f.name)
	}
	var b Any = new(Fd(9))
	if f, ok := b.(owned[File]); ok {
		println("not reached", f.name)
	}
	println("asserted end")
}

type Holder owner struct {
	items [2]Any
	label string
}

func holder() {
	h := Holder{label: "holder"}
	h.items[0] = file("held-0")
	h.items[1] = new(Fd(5))
	println(h.label)
}

// leaving waits for leave's task.
var leaving sync.WaitGroup

// leave ends its task with rt.Goexit: the deferred calls of every frame
// run and what each frame holds is dropped, innermost frame first.
func leave() {
	defer leaving.Done()
	stop()
}

func stop() {
	var a Any = file("goexit")
	var b Any = new(Big{name: "goexit-big"})
	_, _ = a, b
	println("leaving")
	rt.Goexit()
}

func main() {
	scope()
	println(early(0))
	println(early(1))
	reassign()
	take(file("arg"))
	x := pass()
	println("passed", x != nil)
	x = nil
	counted()
	widen()
	asserted()
	holder()
	{
		b := Big{name: "big"}
		var v Any = &b // a view: dropping v drops nothing
		_ = v
		var o Any = new(b) // b moves into a box, which moves into o
		_ = o
	}
	leaving.Add(1)
	task leave()
	leaving.Wait()
	println("end")
}
