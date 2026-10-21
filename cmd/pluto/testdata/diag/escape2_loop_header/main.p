package main

// A variable of a loop's header lives across the iterations; a variable of
// the body is made again by each one (section 18.4). Keeping a view of the
// body's variable in the header's is E3006.
func main() {
	for p, i := (*int)(nil), 0; i < 3; i++ {
		if p != nil {
			println(*p)
		}
		x := i
		p = &x
	}
}
