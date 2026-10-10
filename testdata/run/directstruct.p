package main

// A struct or array that is a single pointer fits in an interface's data
// word, as in Go (language spec section 4.4), so such a value can be
// returned in an interface: nothing is boxed in the returning frame.

type detail struct {
	op   string
	code int
}

// wrapErr is an error that wraps a single pointer.
type wrapErr struct{ d *detail }

func (e wrapErr) Error() string { return e.d.op }
func (e wrapErr) Code() int     { return e.d.code }

// nested wraps a struct that wraps a pointer.
type nested struct{ inner wrapErr }

func (n nested) Error() string { return n.inner.d.op[:2] }

// handle is a one-element array of pointers.
type handle [1]*detail

func (h handle) Error() string { return h[0].op[1:] }

// fnBox holds a single function value.
type fnBox struct{ f func(int) int }

func (b fnBox) Apply(x int) int { return b.f(x) }

type applier interface{ Apply(int) int }

type coder interface {
	error
	Code() int
}

var details = [3]detail{{"open", 2}, {"read", 5}, {"close", 9}}

//go:noinline
func fail(i int) error {
	if i < 0 {
		return nil
	}
	return wrapErr{&details[i]}
}

//go:noinline
func failNested(i int) error { return nested{wrapErr{&details[i]}} }

//go:noinline
func failHandle(i int) error { return handle{&details[i]} }

//go:noinline
func asAny(i int) any { return wrapErr{&details[i]} }

func double(x int) int { return 2 * x }

//go:noinline
func makeApplier() applier { return fnBox{double} }

// clobber overwrites the stack where the callees above had their frames.
func clobber(depth int) int {
	var junk [64]int
	for i := range junk {
		junk[i] = depth*1000 + i
	}
	if depth > 0 {
		return junk[depth] + clobber(depth-1)
	}
	return junk[0]
}

func main() {
	e0 := fail(0)
	e1 := failNested(1)
	e2 := failHandle(2)
	a := asAny(1)
	ap := makeApplier()
	println("clobber", clobber(30) > 0)

	println(e0.Error(), e1.Error(), e2.Error(), ap.Apply(21))
	println("nil error", fail(-1) == nil)

	if c, ok := e0.(coder); ok {
		println("coder", c.Code(), c.Error())
	}
	if w, ok := e0.(wrapErr); ok {
		println("assert", w.d.op, w.d.code, w.d == &details[0])
	}
	if _, ok := e1.(wrapErr); !ok {
		println("nested is not wrapErr")
	}
	switch v := a.(type) {
	case nested:
		println("wrong case", v.inner.d.op)
	case wrapErr:
		println("type switch", v.d.op, v.Code())
	}

	// Equality compares the pointer each value holds.
	println("equal", e0 == fail(0), e0 == fail(1), a == any(wrapErr{&details[1]}))
	println("handle equal", e2 == error(handle{&details[2]}), e2 == error(handle{&details[0]}))

	// A slice of interfaces filled by a callee.
	var errArr [3]error
	errs := errArr[:0]
	for i := 0; i < 3; i++ {
		errs = errs[:i+1]
		errs[i] = fail(i)
	}
	clobber(10)
	for _, e := range errs {
		println("collected", e.Error())
	}
}
