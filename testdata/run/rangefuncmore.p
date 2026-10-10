// Range over functions: generic code, methods, recursive iterators, large
// values, closures in the body, and exits to ordinary loops around it.

package main

type Seq[V any] func(yield func(V) bool)
type Seq2[K, V any] func(yield func(K, V) bool)

type Number interface{ ~int | ~int64 | ~float64 }

func Values[V any](s []V) Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range s {
			if !yield(v) {
				return
			}
		}
	}
}

// Sum ranges over a sequence in generic code.
func Sum[T Number](seq Seq[T]) T {
	var total T
	for v := range seq {
		total += v
	}
	return total
}

// First returns the first value of seq that keep accepts.
func First[T any](seq Seq[T], keep func(T) bool) (T, bool) {
	for v := range seq {
		if keep(v) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

type Tree struct {
	left, right *Tree
	v           int
}

func (t *Tree) walk(yield func(int) bool) bool {
	if t == nil {
		return true
	}
	return t.left.walk(yield) && yield(t.v) && t.right.walk(yield)
}

// All is a recursive iterator: the walk passes yield down.
func (t *Tree) All() Seq[int] {
	return func(yield func(int) bool) {
		t.walk(yield)
	}
}

// Walk has the signature of a sequence itself, so t.Walk is ranged over
// as a method value.
func (t *Tree) Walk(yield func(int) bool) {
	t.walk(yield)
}

type big struct {
	a, b, c, d int
	name      string
}

func bigs(yield func(string, big) bool) {
	for i := 1; i <= 3; i++ {
		b := big{a: i, b: i * 2, c: i * 3, d: i * 4, name: "big"}
		if !yield("k", b) {
			return
		}
	}
}

type counter struct {
	n     int
	total int
}

func (c *counter) add(x int) { c.total += x }

// rangeInMethod ranges in a method, assigning to fields.
func (c *counter) rangeInMethod(seq Seq[int]) {
	for c.n = range seq {
		c.add(c.n)
	}
}

func named(seq Seq[int]) (last int, count int) {
	for v := range seq {
		last = v
		count++
		if v >= 30 {
			return
		}
	}
	return -1, count
}

func closuresInBody() int {
	outer := 100
	sum := 0
	for v := range Values([]int{1, 2, 3}) {
		f := func(k int) int { return v*k + outer }
		sum += f(10)
		inc := func() { sum++ }
		inc()
	}
	return sum
}

func gotoInBody() int {
	n := 0
	for v := range Values([]int{1, 2, 3, 4}) {
		if v%2 == 0 {
			goto even
		}
		n += 100
		continue
	even:
		n += v
	}
	return n
}

func ordinaryLoops() int {
	hits := 0
outer:
	for i := 0; i < 4; i++ {
		for v := range Values([]int{1, 2, 3}) {
			if v == i {
				continue outer
			}
			if i == 3 && v == 2 {
				break outer
			}
			hits += 10*i + v
		}
		hits += 1000
	}
	return hits
}

func switchInBody(seq Seq[int]) int {
	out := 0
	for v := range seq {
		switch {
		case v == 2:
			continue
		case v == 4:
			break
		default:
			out += v
		}
		out += 100
	}
	return out
}

func Concat[V any](seqs ...Seq[V]) Seq[V] {
	return func(yield func(V) bool) {
		for _, seq := range seqs {
			for v := range seq {
				if !yield(v) {
					return
				}
			}
		}
	}
}

// Iterators made during package initialisation outlive it.
var evens = Values([]int{2, 4, 6})
var both = Concat(evens, Values([]int{1, 3}))

func main() {
	for v := range both {
		print(v, " ")
	}
	println(Sum(evens))
	println(Sum(Values([]int{1, 2, 3, 4})), Sum(Values([]float64{0.5, 0.25})) == 0.75)
	v, ok := First(Values([]string{"apple", "banana", "cherry"}), func(s string) bool { return s[0] == 'b' })
	println(v, ok)
	_, ok = First(Values([]int{1, 3}), func(x int) bool { return x%2 == 0 })
	println(ok)

	n1, n3, n5, n7 := Tree{v: 1}, Tree{v: 3}, Tree{v: 5}, Tree{v: 7}
	n2 := Tree{left: &n1, right: &n3, v: 2}
	n6 := Tree{left: &n5, right: &n7, v: 6}
	root := Tree{left: &n2, right: &n6, v: 4}
	for x := range root.All() {
		print(x, " ")
		if x == 6 {
			break
		}
	}
	println()
	total := 0
	for x := range root.Walk {
		total += x
	}
	println("walk", total)

	for k, b := range bigs {
		println(k, b.a, b.b, b.c, b.d, b.name)
		if b.a == 2 {
			break
		}
	}

	var c counter
	c.rangeInMethod(Values([]int{5, 6, 7}))
	println(c.n, c.total)

	println(named(Values([]int{10, 20, 30, 40})))
	println(named(Values([]int{1, 2})))
	println(closuresInBody())
	println(gotoInBody())
	println(ordinaryLoops())
	println(switchInBody(Values([]int{1, 2, 3, 4, 5})))
}
