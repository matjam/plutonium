package main

import (
	"pluto/alloc"
	"pluto/strconv"
)

// take (language spec section 13.10) and the moves of generic code over an
// owner type parameter (section 10.9) with counted values: the references a
// place holds move out with the bytes, so nothing leaks (this program runs
// under the debug allocator's leak report) and nothing is released twice.

func name(i int) string { return "item-" + strconv.Itoa(i) }

type entry struct {
	key  string
	tags [2]string
	n    int
}

type point struct{ x, y int }

// pop2 is Vec.Pop in miniature: a generic function that takes a value out
// of a slice and returns it.
func pop2[T owner any](s []T, i int) (T, bool) {
	x := take(&s[i])
	return x, true
}

func one[T owner any](s []T, i int) T {
	x := take(&s[i])
	return x
}

// swap moves values through parameters and locals.
func swap[T owner any](a, b []T) {
	x := take(&a[0])
	y := take(&b[0])
	a[0] = y
	b[0] = x
}

func main() {
	arr := [3]string{name(1), name(2), name(3)}
	x := take(&arr[0])
	println(x, "|", arr[0], "|", arr[1])

	var ents [2]entry
	ents[0] = entry{key: name(10), tags: [2]string{name(11), name(12)}, n: 5}
	e := take(&ents[0])
	println(e.key, e.tags[0], e.tags[1], e.n, "|", ents[0].key == "", ents[0].n)

	// A discarded take releases what it took.
	_ = take(&arr[1])
	println(arr[1] == "", arr[2])

	a, ok := pop2(arr[:], 2)
	println(a, ok, arr[2] == "")

	strs := [2]string{name(20), name(21)}
	b := one(strs[:], 1)
	println(b, strs[1] == "", strs[0])

	l := []string{name(30)}
	r := []string{name(31)}
	swap(l, r)
	println(l[0], r[0])

	ps := [2]point{{1, 2}, {3, 4}}
	p := one(ps[:], 1)
	println(p.x, p.y, ps[1].x)

	// A shared box taken out of a slot keeps its count.
	boxes := [2]shared[point]{alloc.Shared[point](), alloc.Shared[point]()}
	boxes[0].x = 7
	s := take(&boxes[0])
	println(s.x, boxes[0] == nil)
	s2 := s
	println(s2.x)
}
