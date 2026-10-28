package main

// Closures of calls in a loop kept past the iteration by the functions they
// are passed to, or by their own code (spec section 11.5). Calling such a
// closure stores only what its function's summary says; these are the ways
// a summary records that a closure, or what it holds, is kept.

type Seq = func(func(int) bool)

func count(n int) Seq {
	return func(yield func(int) bool) {
		for i := 0; i < n && yield(i); i++ {
		}
	}
}

func filter(it Seq, keep func(int) bool) Seq {
	return func(yield func(int) bool) {
		it(func(x int) bool { return !keep(x) || yield(x) })
	}
}

func sum(it Seq) int {
	t := 0
	for x := range it {
		t += x
	}
	return t
}

var saved Seq

var keeper = func(s Seq) { saved = s }

// Fine: composed twice and passed to a function that ranges over it, all
// within the iteration.
func Compose(sizes []int) int {
	t := 0
	for _, n := range sizes {
		odd := filter(filter(count(n), func(x int) bool { return x%2 == 1 }), func(x int) bool { return x%3 != 0 })
		for x := range odd {
			t += x
		}
		t += sum(filter(count(n), func(x int) bool { return x%2 == 0 }))
	}
	return t
}

// Assigning to a package variable.
func put(it Seq) { saved = it }

func Put(sizes []int) {
	for _, n := range sizes {
		put(count(n))
	}
}

// The returned closure does, when called.
func stash(it Seq) func() { return func() { saved = it } }

func Stash(sizes []int) {
	for _, n := range sizes {
		stash(count(n))()
	}
}

// Through a function that stores through another pointer.
var slot = new(Seq)

func put2(it Seq) { *slot = it }

func wrap(it Seq) { put2(it) }

func Wrap(sizes []int) {
	for _, n := range sizes {
		wrap(count(n))
	}
}

// Passing it to a func value, which may keep it.
func pass(it Seq) { keeper(it) }

func Pass(sizes []int) {
	for _, n := range sizes {
		pass(count(n))
	}
}

// Through a pointer the returned closure is given.
func stashTo(it Seq) func(*Seq) { return func(p *Seq) { *p = it } }

func StashTo(sizes []int) Seq {
	var out Seq
	for _, n := range sizes {
		stashTo(count(n))(&out)
	}
	return out
}

// The same, from a closure returned in a struct.
type ops struct{ put func(*Seq) }

func mkops(it Seq) ops { return ops{put: func(p *Seq) { *p = it }} }

func StashOps(sizes []int) Seq {
	var out Seq
	for _, n := range sizes {
		mkops(count(n)).put(&out)
	}
	return out
}

// A method value, whose method keeps its receiver's closure.
type holder struct{ it Seq }

func (h holder) keep() { saved = h.it }

func methodOf(it Seq) func() { return holder{it}.keep }

func Method(sizes []int) {
	for _, n := range sizes {
		methodOf(count(n))()
	}
}

// What calling a func parameter returns, stored and returned.
func ret(it Seq) func() Seq { return func() Seq { return it } }

func storeResult(k func() Seq) { saved = k() }

func returnResult(k func() Seq) Seq { return k() }

func StoreResult(sizes []int) {
	for _, n := range sizes {
		storeResult(ret(count(n)))
	}
}

func ReturnResult(sizes []int) Seq {
	var last Seq
	for _, n := range sizes {
		last = returnResult(ret(count(n)))
	}
	return last
}

// The closures in the call's own region, returned or passed on by its
// closure, and kept by the caller or by the function it is passed to.
func own(n int) func() Seq {
	inner := count(n)
	return func() Seq { return inner }
}

func ownGen(n int) func(func(Seq)) {
	inner := count(n)
	return func(f func(Seq)) { f(inner) }
}

func keepAll(k func(func(Seq))) { k(func(s Seq) { saved = s }) }

func Own(sizes []int) Seq {
	var last Seq
	for _, n := range sizes {
		last = own(n)()
	}
	return last
}

func OwnGen(sizes []int) {
	for _, n := range sizes {
		keepAll(ownGen(n))
	}
}

func main() {
	println(Compose([]int{5, 10}))
	Put([]int{1})
	Stash([]int{1})
	Wrap([]int{1})
	Pass([]int{1})
	StashTo([]int{1})
	StashOps([]int{1})
	Method([]int{1})
	StoreResult([]int{1})
	ReturnResult([]int{1})
	Own([]int{1})
	OwnGen([]int{1})
}
