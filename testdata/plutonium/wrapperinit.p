// An untyped constant is assignable to volatile[T] and aligned[T, N] in
// every assignment context when it is assignable to T (spec sections 7.15
// and 7.16).
package main

type S struct {
	v volatile[uint32]
	a aligned[float64, 32]
}

type A aligned[int, 64]

type Color enum {
	Red
	Green
}

var g aligned[int, 64] = 7
var gv volatile[int8] = -3
var gs = S{v: 5, a: 1.5}
var ga A = 9

const c = 6

func f(a aligned[int, 64], b volatile[uint16]) int { return a + int(b) }

func r() volatile[int] { return 7 }

func rv() (volatile[uint8], volatile[bool]) { return 115, true }

func main() {
	var y aligned[int, 64] = 7
	var v volatile[int] = c * 2
	var e volatile[Color] = Color.Green
	s := S{v: 7, a: 2}
	t := S{3, 0.25}
	arr := [2]volatile[int]{1, 2}
	sl := []aligned[int, 16]{3, 4}
	m := map[string]volatile[int]{"k": 5}
	p := &S{v: 8}
	println(g, gv, gs.v, int(gs.a*4), ga)
	println(y, v, e == Color.Green, s.v, int(s.a*4), t.v, int(t.a*4))
	println(f(7, 8), r())
	str, b := rv()
	println(int(str), b)
	println(arr[0], arr[1], sl[0], sl[1], m["k"], p.v)
	y, v = 10, 11
	println(y, v)
}
