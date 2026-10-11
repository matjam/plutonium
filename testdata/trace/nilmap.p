package main

// Storing into a nil map panics, as in Go; reading, deleting and ranging
// over one do not.

type registry struct {
	names map[string]int
}

func (r *registry) add(name string, n int) {
	r.names[name] = n
}

func main() {
	var r registry
	println(r.names["x"], len(r.names))
	delete(r.names, "x")
	for range r.names {
	}
	r.add("first", 1)
	println("not reached")
}
