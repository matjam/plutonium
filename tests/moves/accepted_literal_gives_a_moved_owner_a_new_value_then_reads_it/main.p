package main

// a is moved into eat, then the literal gives it a new value before it
// reads it: every read in the literal follows the new value (language
// spec section 18.5).

type L owner struct{ x int }

func eat(l owned[L]) { println(l.x) }

func main() {
	a := new(L)
	eat(a)
	f := func() {
		a = new(L)
		a.x = 4
		println(a.x)
	}
	f()
}
