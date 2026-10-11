package main

// A field past a large array is beyond the unmapped page at address zero:
// a read of it through a nil pointer is checked explicitly and panics, as a
// read at a small offset faults.

type table struct {
	count int
	slots [100000]int
	last  int
}

func (t *table) first() int {
	return t.count + t.slots[2]
}

func (t *table) final() int {
	return t.last
}

func main() {
	t := &table{count: 1}
	t.slots[2] = 2
	t.last = 3
	println(t.first(), t.final())
	var none *table
	println(none.final())
}
