package main

// Built-in maps: every operation, nil maps, aliasing through assignment and
// calls, and maps held in structs, arrays and other maps. Output never
// depends on iteration order.

type point struct{ x, y int }

type inventory struct {
	name   string
	counts map[string]int
}

func fill(m map[string]int, n int) {
	for i := 0; i < n; i++ {
		m[word(i)] = i
	}
}

func word(i int) string {
	s := ""
	for {
		s = string(rune('a'+i%26)) + s
		i /= 26
		if i == 0 {
			return s
		}
	}
}

func sum(m map[string]int) int {
	t := 0
	for _, v := range m {
		t += v
	}
	return t
}

func keyLen(m map[string]int) int {
	t := 0
	for k := range m {
		t += len(k)
	}
	return t
}

func build(n int) map[int]string {
	m := make(map[int]string, n)
	for i := 0; i < n; i++ {
		m[i] = word(i * 7)
	}
	return m
}

func lookup(m map[int]string, k int) (string, bool) {
	v, ok := m[k]
	return v, ok
}

func main() {
	// make, store, load, comma-ok, len, delete.
	m := make(map[string]int)
	m["one"] = 1
	m["two"] = 2
	m["three"] = 3
	println(len(m), m["one"], m["two"], m["three"], m["four"])
	v, ok := m["two"]
	println(v, ok)
	v, ok = m["four"]
	println(v, ok)
	delete(m, "two")
	delete(m, "missing")
	_, ok = m["two"]
	println(len(m), ok)

	// Compound assignment and increments, on new and existing keys.
	m["one"] += 10
	m["new"] += 5
	m["one"]++
	m["neg"]--
	m["one"] *= 2
	m["one"] <<= 1
	println(m["one"], m["new"], m["neg"], len(m))

	// Aliasing: copies share one table, and a callee changes the caller's
	// map.
	alias := m
	alias["shared"] = 42
	println(m["shared"], len(m) == len(alias))
	fill(m, 100)
	println(len(alias), sum(alias), keyLen(alias))

	// A map returned from a function, and looked up through another.
	b := build(500)
	s, found := lookup(b, 499)
	println(len(b), s, found)
	_, found = lookup(b, 500)
	println(found)

	// nil maps read as empty; delete and clear do nothing.
	var nilm map[string]int
	println(nilm == nil, len(nilm), nilm["x"])
	_, ok = nilm["x"]
	delete(nilm, "x")
	clear(nilm)
	for range nilm {
		println("never")
	}
	println(ok, m != nil)

	// Literals, with composite keys and elided element types.
	pts := map[point]string{{1, 2}: "a", {3, 4}: "b", point{5, 6}: "c"}
	println(len(pts), pts[point{3, 4}], pts[point{1, 2}], pts[point{9, 9}] == "")
	byName := map[string]point{"origin": {}, "unit": {1, 1}}
	println(byName["unit"].x, byName["origin"].y, len(byName))

	// clear empties every alias.
	clear(alias)
	println(len(m), len(alias), m["one"])
	m["again"] = 7
	println(len(alias), alias["again"])

	// Maps in structs and arrays, copied with them.
	inv := inventory{name: "shop", counts: map[string]int{"apple": 3}}
	inv2 := inv
	inv2.counts["pear"] = 2
	println(len(inv.counts), inv.counts["pear"], inv.name)
	var arr [3]map[int]int
	for i := range arr {
		arr[i] = map[int]int{i: i * i}
	}
	arr2 := arr
	arr2[2][9] = 81
	println(len(arr[2]), arr[2][2], arr[2][9], arr[1][1])

	// Reassigning a map variable, and a map of maps.
	nested := map[string]map[string]int{}
	nested["a"] = map[string]int{"x": 1}
	nested["b"] = make(map[string]int)
	nested["b"]["y"] = 2
	nested["a"]["z"] += 3
	inner := nested["a"]
	nested["a"] = nil
	println(len(inner), inner["z"], len(nested["a"]), nested["b"]["y"], len(nested))

	// Large maps grow and keep every entry.
	big := map[int]int{}
	for i := 0; i < 10000; i++ {
		big[i*31] = i
	}
	bad := 0
	for i := 0; i < 10000; i++ {
		if big[i*31] != i {
			bad++
		}
	}
	for i := 0; i < 10000; i += 2 {
		delete(big, i*31)
	}
	for i := 0; i < 10000; i++ {
		_, ok := big[i*31]
		if ok != (i%2 == 1) {
			bad++
		}
	}
	println(len(big), bad)
}
