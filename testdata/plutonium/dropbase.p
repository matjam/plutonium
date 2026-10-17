package main

// Drop of a type defined from an owner, and x.Drop() on every owner
// (language spec sections 7.18 and 18.6): a value of `type B owner A` drops
// as a B (its own Drop, if it declares one) and then as an A (A's Drop, then
// its fields). x.Drop() drops any owner variable now, whether or not its type
// declares Drop. The program runs under the debug allocator, which fails on
// a leak.

type File owner struct{ fd int }

func (f *File) Drop() {
	if f.fd == 0 {
		return
	}
	println("  close", f.fd)
}

// Log declares nothing: dropping it closes the file.
type Log owner File

// Named adds a Drop of its own, which runs first.
type Named owner File

func (n *Named) Drop() { println("  named drop", n.fd) }

// Chain is defined from Log, which is defined from File.
type Chain owner Log

// Plain owns a File and declares no Drop.
type Plain owner struct {
	f File
	n int
}

// Buf has no Drop and holds an allocation.
type Buf owner struct{ data owned[int] }

func newBuf(v int) Buf {
	b := Buf{data: new(int)}
	*b.data = v
	return b
}

func scopeEnd() {
	println("scopeEnd")
	l := Log(File{fd: 1})
	n := Named(File{fd: 2})
	c := Chain(Log(File{fd: 3}))
	_, _, _ = &l, &n, &c
}

func early() {
	println("early")
	l := Log(File{fd: 4})
	l.Drop()
	println("  after l.Drop")
	n := Named(File{fd: 5})
	n.Drop()
	println("  after n.Drop")
	c := Chain(Log(File{fd: 6}))
	c.Drop()
	println("  after c.Drop")
	p := Plain{f: File{fd: 7}, n: 1}
	p.Drop()
	println("  after p.Drop")
	b := newBuf(8)
	b.Drop()
	println("  after b.Drop")
	var arr [2]Plain
	arr[0] = Plain{f: File{fd: 9}}
	arr[1] = Plain{f: File{fd: 10}}
	arr.Drop()
	println("  after arr.Drop")
}

// dropTwice: the dropped variable holds nothing, so the scope end drops
// nothing, and a variable given a new value after the early drop is dropped
// at the scope end.
func dropTwice() {
	println("dropTwice")
	l := Log(File{fd: 11})
	l.Drop()
	l = Log(File{fd: 12})
	println("  reassigned")
}

func deferred() {
	println("deferred")
	p := Plain{f: File{fd: 13}}
	defer p.Drop()
	l := Log(File{fd: 14})
	defer l.Drop()
	println("  body")
}

func overwrite() {
	println("overwrite")
	l := Log(File{fd: 15})
	l = Log(File{fd: 16})
	_ = &l
}

func moved() {
	println("moved")
	l := Log(File{fd: 17})
	take(l)
	println("  after take")
}

func take(l Log) { println("  take", l.fd) }

type Pair[T owner any] owner struct {
	a, b T
}

func generic() {
	println("generic")
	p := Pair[Log]{a: Log(File{fd: 18}), b: Log(File{fd: 19})}
	_ = &p
}

func main() {
	scopeEnd()
	early()
	dropTwice()
	deferred()
	overwrite()
	moved()
	generic()
	println("done")
}
