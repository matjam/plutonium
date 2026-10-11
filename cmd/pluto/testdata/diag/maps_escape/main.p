package main

// A map's table is heap memory that any copy of the map may reach, so it
// cannot keep a view of a frame (language spec sections 7.10 and 18.3), and
// converting a map to an ordinary interface lends it.

type entry struct {
	name string
	data []byte
}

var index = map[string]*int{}

func remember(m map[string]*int, p *int) { m["last"] = p }

func lend() any {
	m := map[string]int{"a": 1}
	return m
}

func main() {
	n := 1
	local := map[string]*int{}
	local["n"] = &n
	remember(index, &n)

	var buf [8]byte
	entries := map[int]entry{1: {name: "x", data: buf[:]}}
	_ = entries

	set := map[*int]bool{}
	set[&n] = true
	_ = lend()
}
