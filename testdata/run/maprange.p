package main

// Ranging over maps with Go's guarantees while the loop changes the map:
// every entry present at the start and not deleted before it is reached is
// visited exactly once, deleted entries are not visited, and added entries
// may or may not be. Every way out of a loop. Output never depends on
// iteration order.

func seq(n int) map[int]int {
	m := map[int]int{}
	for i := 0; i < n; i++ {
		m[i] = i * 10
	}
	return m
}

// filter deletes, while ranging, every key the loop reaches that is a
// multiple of three: the standard Go way to filter a map.
func filter() {
	m := seq(1000)
	visits := map[int]int{}
	for k := range m {
		visits[k]++
		if k%3 == 0 {
			delete(m, k)
		}
	}
	twice := 0
	for _, n := range visits {
		if n != 1 {
			twice++
		}
	}
	println("filter", len(visits), twice, len(m))
}

// deleteOthers deletes entries the loop has not reached yet: they must not
// be visited.
func deleteOthers() {
	m := seq(500)
	seen := map[int]bool{}
	deleted := map[int]bool{}
	for k := range m {
		seen[k] = true
		other := (k + 250) % 500
		if !seen[other] && !deleted[other] {
			delete(m, other)
			deleted[other] = true
		}
	}
	bad := 0
	for k := range deleted {
		if seen[k] {
			bad++
		}
	}
	println("deleteOthers", len(seen)+len(deleted), bad, len(m))
}

// grow adds many entries while ranging, so that the table is rebuilt more
// than once during the loop: every original entry is still visited exactly
// once.
func grow() {
	m := seq(64)
	visits := map[int]int{}
	for k := range m {
		visits[k]++
		if k < 64 {
			for j := 0; j < 100; j++ {
				m[1000+k*100+j] = j
			}
		}
	}
	bad := 0
	for i := 0; i < 64; i++ {
		if visits[i] != 1 {
			bad++
		}
	}
	for k, n := range visits {
		if n != 1 || k >= 64 && k < 1000 {
			bad++
		}
	}
	println("grow", len(m), bad)
}

// growAndDelete rebuilds the table during the loop and deletes entries not
// reached yet after the rebuild.
func growAndDelete() {
	m := seq(100)
	visited := map[int]bool{}
	for k := range m {
		if k >= 100 {
			continue
		}
		if visited[k] {
			println("twice", k)
		}
		visited[k] = true
		if len(visited) == 10 {
			for j := 0; j < 1000; j++ {
				m[100+j] = j
			}
			for j := 0; j < 100; j++ {
				if !visited[j] && j%2 == 0 {
					delete(m, j)
				}
			}
		}
	}
	bad := 0
	for j := 0; j < 100; j++ {
		_, there := m[j]
		if visited[j] != there {
			bad++
		}
	}
	println("growAndDelete", bad)
}

// updates stores into entries during the loop: a later entry is seen with
// its new value.
func updates() {
	m := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	total := 0
	for k, v := range m {
		total += v
		for other := range m {
			if other != k {
				m[other] += 100
			}
		}
		m[k] = 0
	}
	println("updates", total >= 10, total <= 10+3*400)
}

func firstOver(m map[string]int, limit int) bool {
	for _, v := range m {
		if v > limit {
			return true
		}
	}
	return false
}

func exits() {
	m := map[string]int{"x": 1, "y": 5, "z": 9}
	n := 0
	for range m {
		n++
		if n == 2 {
			break
		}
	}
	count := 0
	for k := range m {
		if k == "y" {
			continue
		}
		count++
	}
	println("exits", n, count, firstOver(m, 4), firstOver(m, 9))

	i := 0
outer:
	for a := range m {
		for b := range m {
			if a != b {
				i++
				continue outer
			}
		}
	}
	println("labels", i)

	tries := 0
again:
	for k := range m {
		if k != "" && tries < 3 {
			tries++
			goto again
		}
	}
	println("goto", tries)
}

// reassign gives the map variable a new map inside the loop: the loop walks
// the map it started with.
func reassign() {
	m := map[int]int{1: 1, 2: 2, 3: 3}
	n := 0
	for k, v := range m {
		n += k * v
		m = map[int]int{100: 100}
	}
	println("reassign", n, len(m))
}

// assign ranges into variables declared outside the loop, and into a map
// element.
func assign() {
	m := map[string]int{"k": 7}
	var k string
	var v int
	for k, v = range m {
	}
	dst := map[string]int{}
	for k = range m {
		dst[k] = len(k)
	}
	println("assign", k, v, dst["k"])
}

// clearing ranges while clearing the map: nothing more is visited.
func clearing() {
	m := seq(100)
	n := 0
	for range m {
		n++
		clear(m)
	}
	println("clearing", n, len(m))
}

func main() {
	filter()
	deleteOthers()
	grow()
	growAndDelete()
	updates()
	exits()
	reassign()
	assign()
	clearing()
	total := 0
	f := func(m map[int]int) {
		for _, v := range m {
			total += v
		}
	}
	f(seq(10))
	println("closure", total)
}
