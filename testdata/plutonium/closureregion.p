package main

// A closure region owns the variables with cleanup its closure captures
// (language spec section 11.5, abi.md section 4.1, D60): it drops them when
// it is refilled and when the caller's frame returns, once each. A
// captured parameter is copied into the region on entry. Every Drop prints.

type R owner struct{ n int }

func (r *R) Drop() { println("drop", r.n) }

func counter(start int) func() int {
	r := R{n: start}
	return func() int {
		r.n++
		return r.n
	}
}

func fromParam(r R) func() int {
	return func() int {
		r.n += 10
		return r.n
	}
}

func maybe(b bool) func() int {
	if b {
		r := R{n: 50}
		return func() int { return r.n }
	}
	return nil
}

func main() {
	c := counter(1)
	println(c())
	println(c())
	d := fromParam(R{n: 100})
	println(d())
	for i := 0; i < 2; i++ {
		e := counter(10 * i)
		println(e())
	}
	f := maybe(false)
	println(f == nil)
	g := maybe(true)
	println(g())
	println("end")
}
