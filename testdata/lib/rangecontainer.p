// Range over library containers (language spec section 6.7): a range
// over a vec.Vec or a hash.Map ranges over its All method, as a range
// over a Go slice or map does.
package main

import (
	"pluto/container"
	"pluto/hash"
	"pluto/iter"
	"pluto/sort"
	"pluto/vec"
)

// A user type with an All method on a value receiver.
type countdown struct{ from int }

func (c countdown) All() iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := c.from; i > 0; i-- {
			if !yield(i) {
				return
			}
		}
	}
}

// sum ranges over any container with an All method returning a Seq2,
// decided from the constraint.
func sum[C container.Iterable[K, int], K any](c C) int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

// count ranges over a type parameter constrained by a method set alone.
func count[C interface{ All() iter.Seq[int] }](c C) int {
	n := 0
	for range c {
		n++
	}
	return n
}

func main() {
	v := vec.New[int]()
	defer v.Free()
	for i := 0; i < 5; i++ {
		v.Push(i * i)
	}

	// Like a slice: index and element, index only, element only, none.
	for i, x := range v {
		println("v", i, x)
	}
	for i := range v {
		print(" ", i)
	}
	println()
	total := 0
	for _, x := range v {
		total += x
	}
	println("total", total)
	n := 0
	for range v {
		n++
	}
	println("n", n)

	// break, continue and assignment to existing variables.
	var i, x int
	for i, x = range v {
		if x == 1 {
			continue
		}
		if x > 5 {
			break
		}
		println("assigned", i, x)
	}
	println("after", i, x)

	// Through a pointer, and the body changing elements.
	p := &v
	for i, x := range p {
		v[i] = x + 1
	}
	println("v[4]", v[4])

	// Like a map: sorted keys, since the order is the table's.
	m := hash.NewMap[string, int](0)
	defer m.Free()
	m["one"] = 1
	m["two"] = 2
	m["three"] = 3
	var ks [3]string
	k := 0
	for key, val := range m {
		ks[k] = key
		k++
		if m[key] != val {
			println("mismatch", key)
		}
	}
	sort.Strings(ks[:])
	for _, key := range ks {
		println("m", key, m[key])
	}
	keys := 0
	for range m {
		keys++
	}
	println("keys", keys)

	// A return from inside a range over a container.
	println("found", find(&m, 2))

	// A set ranges over its elements.
	s := hash.NewSet[int](0)
	defer s.Free()
	s.Add(7)
	for e := range s {
		println("set", e)
	}

	// A user type, and generic code.
	for c := range (countdown{3}) {
		print(" ", c)
	}
	println()
	println("sum vec", sum(&v), "sum map", sum(&m))
	println("count", count(countdown{4}))
}

func find(m *hash.Map[string, int], want int) string {
	for k, v := range m {
		if v == want {
			return k
		}
	}
	return ""
}
