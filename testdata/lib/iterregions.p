// Library iterators returned through functions, used as func values and
// method values, and through interfaces: their closures are built in the
// regions callers provide (language spec section 4.5).
package main

import (
	"pluto/hash"
	"pluto/iter"
	"pluto/slices"
	"pluto/vec"
)

// evens returns an adaptor over an adaptor over a library iterator.
func evens(s []int) iter.Seq[int] {
	return iter.Filter(func(x int) bool { return x%2 == 0 }, slices.Values(s))
}

// doubled maps over evens, three levels down.
func doubled(s []int) iter.Seq[int] {
	return iter.Map(func(x int) int { return 2 * x }, evens(s))
}

// firstN checks its argument before returning the iterator.
func firstN(s []int, n int) iter.Seq[int] {
	if n < 0 {
		n = 0
	}
	return iter.Limit(slices.Values(s), n)
}

// Ranger is satisfied by *vec.Vec[int].
type Ranger interface {
	All() iter.Seq2[int, int]
}

func sum2(r Ranger) int {
	t := 0
	for _, x := range r.All() {
		t += x
	}
	return t
}

func main() {
	s := []int{1, 2, 3, 4, 5, 6}
	for x := range evens(s) {
		print(x, " ")
	}
	println()
	for x := range doubled(s) {
		print(x, " ")
	}
	println()
	println(iter.Count(firstN(s, 4)), iter.Count(firstN(s, -1)))

	for c := range slices.Chunk(s, 4) {
		println(len(c), c[0])
	}

	v := vec.New[int]()
	defer v.Free()
	v.Append(10, 20, 30)
	all := v.All
	for i, x := range all() {
		print(i, "=", x, " ")
	}
	println()
	println(sum2(&v))

	m := hash.NewMap[string, int](4)
	defer m.Free()
	m.Set("a", 1)
	m.Set("b", 2)
	keys := iter.Keys(m.All())
	n := 0
	for range keys {
		n++
	}
	values := slices.Values[[]int]
	println(n, iter.Count(values(s)))
}
