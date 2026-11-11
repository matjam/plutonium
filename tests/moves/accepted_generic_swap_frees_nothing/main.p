package main

// The twin of rejected_generic_store_may_free. A generic function that
// only exchanges the elements of a slice (a[i], a[j] = a[j], a[i]) frees
// nothing for any type argument: every old value is still held by the
// slice afterwards. So a view of a local owner passed to it, or to a sort,
// stays usable (language spec section 18.3, D81).

import (
	"pluto/alloc"
	"pluto/sort"
	"pluto/vec"
)

func swap[T any](data []T) { data[0], data[1] = data[1], data[0] }

func main() {
	v := vec.New[shared[vec.Vec[int]]]()
	v.Push(alloc.Shared[vec.Vec[int]]())
	v.Push(alloc.Shared[vec.Vec[int]]())
	all := v.Slice()
	swap(all)
	println(len(all))
	sort.SortFunc(all, func(a, b shared[vec.Vec[int]]) bool { return false })
	println(len(all))
}
