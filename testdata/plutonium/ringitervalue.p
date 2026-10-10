// A ring's iterator methods used other than by calling them: as method
// values, method expressions, through an interface, deferred and started
// with go. The iterator's closure is built in the region the caller
// provides (language spec sections 4.5 and 5.4).
package main

type Seq interface{ All() func(func(int, int) bool) }

func fill(r *ring[int, 4], n int) {
	for i := 0; i < n; i++ {
		r.Push(i * 10)
	}
}

// iterOf returns the iterator a method value makes.
func iterOf(f func() func(func(int) bool)) func(func(int) bool) {
	return f()
}

func main() {
	var r ring[int, 4]
	fill(&r, 3)

	all := r.All
	for i, x := range all() {
		println("method value", i, x)
	}

	values := (*ring[int, 4]).Values
	for x := range values(&r) {
		println("method expression", x)
	}

	var s Seq = &r
	for i, x := range s.All() {
		println("interface", i, x)
	}

	for x := range iterOf(r.Values) {
		println("returned", x)
	}

	defer r.All()
	fill(&global, 2)
	go global.Values()
	println("done")
}

var global ring[int, 4]
