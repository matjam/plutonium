package main

// The switch expression must be comparable, with or without case clauses
// (spec section 12.9). Slices, maps and funcs compare only to nil, which
// does not make them comparable.

func main() {
	s := []int{1}
	m := map[int]int{}
	f := func() {}
	switch s {
	default:
		println("slice")
	}
	switch m {
	}
	switch f {
	default:
	}
	switch s {
	case nil:
	}
	var p *int
	switch p {
	case nil:
	}
}
