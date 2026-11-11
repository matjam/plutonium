package main

// run calls f, which it may set to keep before the call: the call is not
// only a call of what run was given, so what keep does with o is run's
// own effect, and passing it the address of a frame variable is E3003
// (language spec section 18.3).

type Out struct{ n int }

var kept *Out

func keep(o *Out, i int) int {
	kept = o
	return i
}

func fill(o *Out, i int) int { return i }

func run(f func(*Out, int) int, i int, o *Out, b bool) {
	if b {
		f = keep
	}
	o.n = f(o, i)
}

func main() {
	{
		var o Out
		run(fill, 1, &o, true)
	}
	println(kept.n)
}
