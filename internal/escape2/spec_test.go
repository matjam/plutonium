package escape2

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
)

// lineSites returns the errors of err as "line:col code", in order.
func lineSites(err error) []string {
	var out []string
	add := func(d *diag.Diagnostic) {
		out = append(out, fmt.Sprintf("%d:%d %s", d.Pos.Line, d.Pos.Column, d.Code))
	}
	switch e := err.(type) {
	case *diag.List:
		for _, d := range *e {
			add(d)
		}
	case *diag.Diagnostic:
		add(e)
	}
	return out
}

// checkSpec checks that the analysis reports exactly want on src: the
// errors the spec section a case names requires, and no others.
func checkSpec(t *testing.T, name, src string, want ...string) {
	t.Helper()
	err := Check(loadSrc(t, src))
	if err != nil && isInternal(err) {
		t.Fatalf("%s: %v", name, err)
	}
	if got := lineSites(err); !slices.Equal(got, want) {
		t.Errorf("%s: errors %v, want %v\n%v", name, got, want, err)
	}
}

// A view of an owner temporary that a pointer method is called on may be
// passed to calls in its statement and may not be kept (section 11.14,
// Calls; section 18.3, Views; section 18.6, Drops), whether or not the
// owner type has anything to drop and whether or not the variable that
// keeps it is read again.
func TestOwnerTempViewKept(t *testing.T) {
	for _, drop := range []string{"", "func (b *Buf) Drop() {}\n"} {
		src := `package main

type Buf owner struct {
	data [4]int
	n    int
}

func (b *Buf) Slice() []int { return b.data[:b.n] }

func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}

func sum(s []int) int { return len(s) }

func main() {
	println(sum(readAll(3).Slice()))
	if s := readAll(3).Slice(); len(s) > 0 {
		println(s[0])
	}
	s := readAll(3).Slice()
	println(len(s))
	u := readAll(3).Slice()
	_ = u
	for v := readAll(2).Slice(); len(v) > 0; v = v[1:] {
		println(v[0])
	}
}
` + drop
		name := "no Drop"
		if drop != "" {
			name = "Drop"
		}
		checkSpec(t, name, src, "23:2 E3009", "25:2 E3009", "27:6 E3009")
	}
}

// The address of a local sync.WaitGroup must not reach wg.Task, whose task
// keeps it until Done, which may come after the frame is gone (section
// 12.12, Task statements: "Passing the address of a local sync.WaitGroup
// is this error"; section 19.1 applies those rules to wg.Task), whether or
// not the function waits. A package-level WaitGroup is fine.
func TestLocalWaitGroupTask(t *testing.T) {
	src := `package main

import "pluto/sync"

func work() {}

var g sync.WaitGroup

func local(wait bool) {
	var wg sync.WaitGroup
	wg.Task(work)
	if wait {
		wg.Wait()
	}
}

func global() {
	g.Task(work)
	g.Wait()
}

func main() {
	local(true)
	global()
}
`
	checkSpec(t, "wait group", src, "11:2 E3004")
	err := Check(loadSrc(t, src))
	if want := "cannot pass &wg to sync.(*WaitGroup).Task, which starts a task with it"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("message: %v, want %q", err, want)
	}
}

// A counted box in the caller's region, captured by a closure the function
// returns (section 11.5), may be read through in the closure: the closure
// reaches it through its record, so a view it makes is of heap memory,
// which generation checks guard (section 18.3, "Heap memory reached any
// other way"). The function itself may still not return a view of what
// the box refers to (E3001).
func TestRegionBoxView(t *testing.T) {
	src := `package main

import "pluto/alloc"

type Holder owner struct{ id int }

func fresh() shared[Holder] { return alloc.Shared[Holder]() }

func region() (r shared[Holder], f func() int) {
	r = fresh()
	f = func() int { return r.id }
	return
}

func leak() (f func(), p *int) {
	r := fresh()
	f = func() { println(r.id) }
	p = &r.id
	return
}

func main() {
	_, f := region()
	println(f())
	g, p := leak()
	g()
	println(*p)
}
`
	checkSpec(t, "region box", src, "19:2 E3001")
}
