// Comparison of arrays too large to unroll whose elements are not plain
// bytes (strings, floats, structs, interfaces, arrays), directly, through
// comparable type parameters and inside interfaces.
package main

type P struct {
	s string
	f float64
}

func Eq[T comparable](a, b T) bool { return a == b }

func Index[T comparable](xs []T, v T) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

func main() {
	var a, b [12]string
	println(Eq(a, b), a == b)
	b[11] = "x"
	println(Eq(a, b), a != b)
	a[11] = "x"
	println(Eq(a, b))
	var f1, f2 [9]float64
	f2[0] = -0.0
	println(f1 == f2, Eq(f1, f2))
	var p1, p2 [10]P
	p2[5].s = "q"
	println(p1 == p2, Eq(p1, p1))
	var i1, i2 [20]any
	i1[3], i2[3] = 1, 1
	println(i1 == i2, Eq(i1, i2))
	grid := [][9]string{{}, {"a"}, {8: "z"}}
	println(Index(grid, [9]string{8: "z"}), Index(grid, [9]string{"b"}))
	var big1, big2 [3][10]string
	big2[2][9] = "w"
	println(big1 == big2, Eq(big1, big1))
	println(Eq[any](a, b), Eq[any](big1, big2), Eq[any](p1, p1))
}
