package main

// run2 keeps what f returns, and the literal passed returns its argument:
// run2 keeps the address of the frame variable o, E3003 (language spec
// section 18.3).

type Out struct{ n int }

var kept *Out

func run2(f func(*Out) *Out, o *Out) { kept = f(o) }

func main() {
	{
		var o Out
		run2(func(o *Out) *Out { return o }, &o)
	}
	println(kept.n)
}
