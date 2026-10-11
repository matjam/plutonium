package main

// Maps in generic code, named map types with methods, closures that
// capture maps, and maps as results, fields and elements.

type Set map[string]bool

func (s Set) Add(k string)      { s[k] = true }
func (s Set) Has(k string) bool { return s[k] }
func (s Set) Len() int          { return len(s) }

type Graph struct {
	edges map[int][]int
	names map[int]string
}

func NewGraph() Graph {
	return Graph{edges: map[int][]int{}, names: make(map[int]string)}
}

func (g *Graph) Name(n int, s string) { g.names[n] = s }

func count[K comparable](keys []K) map[K]int {
	m := make(map[K]int, len(keys))
	for _, k := range keys {
		m[k]++
	}
	return m
}

func get[M ~map[K]V, K comparable, V any](m M, k K) (V, bool) {
	v, ok := m[k]
	return v, ok
}

func size[M ~map[K]V, K comparable, V any](m M) int { return len(m) }

func sumValues[K comparable](m map[K]int) int {
	t := 0
	for _, v := range m {
		t += v
	}
	return t
}

func invert[K, V comparable](m map[K]V) map[V]K {
	out := make(map[V]K, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

func drop[K comparable, V any](m map[K]V, keys ...K) {
	for _, k := range keys {
		delete(m, k)
	}
}

func reset[M ~map[K]V, K comparable, V any](m M) { clear(m) }

type pair[K comparable, V any] struct {
	m   map[K]V
	key K
}

func (p pair[K, V]) value() V { return p.m[p.key] }

func main() {
	s := Set{}
	s.Add("a")
	s.Add("b")
	s.Add("a")
	println(s.Len(), s.Has("a"), s.Has("z"))

	g := NewGraph()
	g.Name(1, "one")
	g.edges[1] = nil
	println(g.names[1], len(g.edges))

	words := [6]string{"x", "y", "x", "z", "x", "y"}
	c := count(words[:])
	println(c["x"], c["y"], c["z"], len(c), sumValues(c))
	nums := [4]int{3, 3, 1, 3}
	cn := count(nums[:])
	println(cn[3], cn[1], len(cn))

	v, ok := get(s, "b")
	println(v, ok, size(s), size(c))
	inv := invert(map[string]int{"one": 1, "two": 2})
	println(inv[1], inv[2], len(inv))
	drop(c, "x", "nope")
	println(len(c), c["x"])
	reset(s)
	println(s.Len())

	p := pair[string, int]{m: map[string]int{"k": 5}, key: "k"}
	println(p.value())

	// A closure that keeps a map and changes it.
	seen := map[int]bool{}
	mark := func(n int) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		return true
	}
	fresh := 0
	for _, n := range [7]int{1, 2, 1, 3, 2, 4, 1} {
		if mark(n) {
			fresh++
		}
	}
	println(fresh, len(seen))

	// A slice of maps, and a map value that is a struct of maps.
	var arr [2]map[string]int
	ms := arr[:]
	for i := range ms {
		ms[i] = map[string]int{"i": i}
	}
	println(ms[0]["i"], ms[1]["i"])
	byKind := map[string]Graph{"g": {edges: map[int][]int{7: nil}}}
	println(len(byKind["g"].edges), byKind["g"].names == nil)
}
