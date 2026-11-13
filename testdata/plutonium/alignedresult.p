package main

// A result of type aligned[T, N] has T's value wherever the caller uses
// it: assigned, printed, in an expression, passed on (spec section 7.16).

type pt struct{ x, y int }

type big struct{ a [40]int }

func r() aligned[int, 64]          { return 41 }
func b() aligned[uint8, 16]        { return 7 }
func trio() aligned[[3]uint32, 16] { return [3]uint32{1, 2, 3} }
func p() aligned[pt, 64]           { return pt{3, 4} }
func s() aligned[string, 64]       { return "str" }
func g() aligned[big, 512]         { var v big; v.a[39] = 9; return v }
func two() (aligned[int, 32], int) { return 5, 6 }

func id(v aligned[int, 64]) aligned[int, 64] { return v }

type T struct{ n int }

func (t T) get() aligned[int, 128] { return t.n }

func main() {
	v := r()
	println(v, r(), r()+1)
	var w aligned[int, 64] = r()
	println(w, id(r()), id(w)+2)
	println(b(), b()+1)
	t := trio()
	println(t[0], t[1], t[2], trio()[2])
	println(p().x, p().y)
	q := p()
	println(q.x + q.y)
	println(s(), len(s()))
	println(g().a[39])
	x, y := two()
	println(x, y, x+y)
	println(T{8}.get(), T{8}.get()*2)
}
