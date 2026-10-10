// A return that comes before, in the source, what the function keeps
// until it returns (an interface box, the region of a call that returns a
// closure), but runs after a loop iteration or a goto back has filled it.
// The return must release that too; under --alloc=debug nothing leaks
// (internal/build/strcount_test.go). Found by tools/difftest.
package main

var base = "abc"

//pu:noinline
func boxInLoop(p string, n int) int {
	for j := 0; ; j++ {
		if j == n {
			return j
		}
		var a any = p[1:]
		println(a.(string))
	}
}

func counter(s string) func() int {
	return func() int { return len(s) }
}

//pu:noinline
func regionInLoop(p string, n int) int {
	t := 0
	for j := 0; ; j++ {
		if j == n {
			return t
		}
		c := counter(p + "x")
		t += c()
	}
}

//pu:noinline
func boxAfterGoto(p string, n int) int {
	j := 0
again:
	if j == n {
		return j
	}
	var a any = p[:j]
	println(a.(string))
	j++
	goto again
}

type named struct {
	s string
}

func (v named) get() int { return len(v.s) }

//pu:noinline
func methodValueInLoop(p string, n int) (r int) {
	for j := 0; j <= n; j++ {
		if j == n {
			return r
		}
		f := named{p + "!"}.get
		r += f()
	}
	return -1
}

func main() {
	x := base + "def"
	println(boxInLoop(x, 0), boxInLoop(x, 1), boxInLoop(x, 3))
	println(regionInLoop(x, 0), regionInLoop(x, 2))
	println(boxAfterGoto(x, 0), boxAfterGoto(x, 3))
	println(methodValueInLoop(x, 2))
}
