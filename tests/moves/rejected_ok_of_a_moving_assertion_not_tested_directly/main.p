package main

// The move check follows the ok of a moving comma-ok assertion only
// through a direct test of it (language spec sections 11.13 and 18.5,
// D47). Elsewhere the interface variable is possibly moved (E3302).

type Buf owner struct{ n int }

func (b *Buf) Drop() { println("drop", b.n) }

type Thing owner interface{ Len() int }

func (b *Buf) Len() int { return b.n }

func use(x Thing) { println("use", x.Len()) }

// Through another variable.
func throughVariable(x Thing) {
	v, ok := x.(owned[Buf])
	failed := !ok
	if failed {
		use(x)
		return
	}
	println(v.Len())
}

// No test at all.
func untested(x Thing) {
	_, ok := x.(owned[Buf])
	println(ok)
	use(x)
}

// After an if ok that does not return.
func afterIf(x Thing) {
	if v, ok := x.(owned[Buf]); ok {
		println(v.Len())
	}
	use(x)
}

// ok was given a new value before the test.
func reassigned(x Thing, y bool) {
	v, ok := x.(owned[Buf])
	ok = y
	if !ok {
		use(x)
		return
	}
	println(v.Len())
}

func main() {
	throughVariable(new(Buf{n: 1}))
	untested(new(Buf{n: 1}))
	afterIf(new(Buf{n: 1}))
	reassigned(new(Buf{n: 1}), true)
}
