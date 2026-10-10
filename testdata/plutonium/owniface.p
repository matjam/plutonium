package main

// Owning interfaces (language spec section 4.4): type R owner interface
// {...} owns what is converted into it and drops it when it is dropped,
// whatever its dynamic type. Ordinary interfaces only lend. Every Drop
// prints, so the output shows when each value goes; the program also runs
// under the debug allocator, which fails on a leak.

import "pluto/alloc"

type Resource owner interface {
	Name() string
}

type File owner struct {
	name string
}

func (f *File) Name() string { return f.name }
func (f *File) Drop()        { println("close", f.name) }

// Big is larger than a word: in an owning interface it is held in the
// frame of the function that converts it, so that interface stays there.
type Big owner struct {
	name string
	pad  [4]int
}

func (b *Big) Name() string { return b.name }
func (b *Big) Drop() {
	if b.name != "" { // a moved-from Big is the zero value, and holds nothing
		println("drop big", b.name)
	}
}

type Fd owner int32

func (f *Fd) Name() string { return "fd" }
func (f *Fd) Drop()        { println("drop fd", int32(*f)) }

type Plain struct{ s string }

func (p Plain) Name() string { return p.s }

type Node owner struct{ n string }

func (n *Node) Name() string { return n.n }
func (n *Node) Drop()        { println("drop node", n.n) }

// Pool is an owner with an owning interface field.
type Pool owner struct {
	res  Resource
	name string
}

func open(name string) Resource {
	f := new(File)
	f.name = name
	return f // the owned[File] moves into the interface, then to the caller
}

func use(r Resource) {
	println("using", r.Name())
} // r, the parameter, is dropped here

func describe(r Resource) string {
	switch v := r.(type) {
	case owned[File]:
		return "file " + v.name
	case owned[Fd]:
		return "fd"
	default:
		return "other " + v.Name()
	}
} // the clause's v holds the value and drops it

func main() {
	r := open("a")
	println(r.Name())
	var x Resource = open("b")
	x = open("c") // drops b
	println(x.Name())
	use(open("d"))
	{
		var fd Resource = new(Fd(7))
		println(fd.Name())
	}
	var p Resource = Plain{"plain"}
	println(p.Name())
	s := alloc.Shared[Node]()
	s.n = "shared"
	{
		var sr Resource = s // a reference of its own
		println(sr.Name())
	}
	println("still", s.n)
	{
		b := Big{name: "big"}
		var br Resource = &b // a view: dropping br drops nothing
		println(br.Name())
		var owned Resource = new(b) // b moves into a box, which moves into owned
		println(owned.Name())
	}

	// Assertions move the owner out where they succeed.
	if f, ok := r.(owned[Fd]); ok {
		println("not reached", f.Name())
		r = f // gives r a value again on this path
	} else {
		println("still", r.Name())
	}
	if f, ok := r.(owned[File]); ok {
		println("asserted", f.name)
	}

	println(describe(open("e")))
	println(describe(new(Fd(8))))
	println(describe(Plain{"value"}))

	pool := Pool{res: open("pooled"), name: "pool"}
	println(pool.name, pool.res.Name())
	println("end")
}
