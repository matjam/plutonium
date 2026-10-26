package main

// A struct implements Getter by embedding a Getter; the func is held by an
// interface field whose own implementers include a func type defined in a
// function (local type).
type Getter interface{ Get() int }

type W struct{ Getter }

func mk(x int) Getter {
	type HF func() int
	return W{Getter: hf(func() int { return x * 10 })}
}

type hf func() int

func (h hf) Get() int { return h() }

func outer(x int) Getter { return mk(x) }

//pu:noinline
func clobber(n int) int {
	var a [64]int
	for i := range a {
		a[i] = n + i
	}
	s := 0
	for i := range a {
		s += a[i]
	}
	return s
}

func main() {
	g := outer(7)
	println(clobber(1000))
	println(g.Get())
}
