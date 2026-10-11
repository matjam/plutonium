package main

// Or clauses with owners, strings and boxes (language spec section 11.27):
// where the call fails, the results other than the error and the
// statement's temporaries are dropped before the clause runs, the old
// value of an owner on the left is not dropped, and leaving the clause
// drops the scopes it leaves and joins their sync.Scope. Every Drop
// prints; the program also runs under the debug allocator, which fails on
// a leak (internal/build/or_test.go).

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/strconv"
	"pluto/strings"
	"pluto/sync"
	"pluto/sync/atomic"
)

var errBad = errors.New("bad")

type File owner struct{ name string }

func (f *File) Drop() {
	if f.name != "" { // the zero value, left by a move, holds nothing
		println("close", f.name)
	}
}

type Node owner struct{ name string }

func (n *Node) Drop() { println("drop node", n.name) }

// open returns a file and, for a name starting with x, an error as well:
// the file is dropped where the caller's clause runs.
func open(name string) (File, error) {
	if strings.HasPrefix(name, "x") {
		return File{name: name}, errBad
	}
	return File{name: name}, nil
}

// text returns a string made at run time, and an error for a negative n.
func text(n int) (string, error) {
	s := strings.Repeat("ab", 3) + strconv.Itoa(n)
	if n < 0 {
		return s, errBad
	}
	return s, nil
}

func node(name string, fail bool) (shared[Node], error) {
	n := alloc.Shared[Node]()
	n.name = name
	if fail {
		return n, errBad
	}
	return n, nil
}

func use(name string, f File) error {
	println("use", name, f.name)
	return nil
}

func check(s string) error {
	if s == "" {
		return errBad
	}
	return nil
}

// declare: the values are declared where the call succeeds and dropped
// before the clause where it fails.
func declare(name string) error {
	f := open(name) or {
		println("clause", name)
		return err
	}
	println("opened", f.name)
	return nil
}

// assign: the old value of an owner on the left is dropped only where the
// call succeeds.
func assign() {
	f := File{name: "first"}
	f = open("second") or panic(err) // drops first
	println("now", f.name)
	f = open("xthird") or {
		println("kept", f.name) // xthird is dropped, second is kept
		return
	}
	println("not reached")
}

// discard: without a left side, the values are dropped as the results of
// an expression statement are.
func discard() error {
	open("discarded") or return
	open("xfailed") or return
	return nil
}

// strs: strings made at run time are released on both paths.
func strs() (string, error) {
	s := text(1) or return
	t := text(-1) or return s + "!", err
	return t, nil
}

// temps: the temporaries of the call's operands are dropped before the
// clause runs.
func temps() error {
	check(strings.Repeat("z", 2)) or return
	check(strings.Repeat("z", 0)) or {
		println("temps clause")
		return err
	}
	return nil
}

// boxes: a shared box among the values is released where the call fails.
func boxes() (int, error) {
	a := node("kept", false) or return
	b := node("failed", true) or {
		println("box clause", a.name)
		return 0, err
	}
	println("not reached", b.name)
	return 1, nil
}

// moved: a variable moved into the call is moved on both paths.
func consume(f File, fail bool) (int, error) {
	println("consume", f.name)
	if fail {
		return 0, errBad
	}
	return 1, nil
}

func moved(fail bool) error {
	f := File{name: "moved"}
	consume(f, fail) or return // f was moved: nothing to drop on either path
	return nil
}

// scopes: a return from a clause joins the sync.Scope of the blocks it
// leaves before the function returns.
var done atomic.Int64

func spin(n int) int {
	x := 0
	for i := 0; i < n*1000; i++ {
		x += i & 3
	}
	return x
}

func scopes(fail bool) (int, error) {
	var cells [3]int
	{
		var s sync.Scope
		for i := range cells {
			s.Task(func() {
				cells[i] = spin(i+1)*0 + i
				done.Add(1)
			})
		}
		check(map[bool]string{true: "", false: "ok"}[fail]) or return 0, err
	}
	return cells[0] + cells[1] + cells[2], nil
}

// loops: break and continue from clauses drop the iteration's owners.
func loops() {
	for _, name := range []string{"a", "xb", "c", "xd"} {
		f := File{name: "iter " + name}
		g := open(name) or continue
		println("loop", g.name, f.name)
	}
	for _, name := range []string{"e", "xf", "g"} {
		f := File{name: "iter " + name}
		g := open(name) or break
		println("loop", g.name, f.name)
	}
}

func main() {
	declare("plain")
	declare("xbad")
	assign()
	discard()
	s, e := strs()
	println("strs", s, e != nil)
	temps()
	_, e = boxes()
	println("boxes", e != nil)
	moved(false)
	moved(true)
	n, e := scopes(false)
	println("scopes", n, e == nil, done.Load())
	n, e = scopes(true)
	println("scopes", n, e != nil, done.Load())
	loops()
	println("done")
}
