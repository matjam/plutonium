package main

// A freestanding program has no allocator while its packages are
// initialized, so an initializer that allocates is an error (spec section
// 15.2). Static initializers are fine.

import (
	"pluto/errors"
	"pluto/rt"
)

type node struct {
	name string
	next *node
}

var (
	ErrBad = errors.New("bad")   // static
	root   = &node{name: "root"} // static
	cache  = make(map[string]int)
	names  = loadNames()
)

func loadNames() []string {
	seen := map[string]bool{}
	_ = seen
	return nil
}

var table map[int]int

func init() {
	table = make(map[int]int)
}

//pu:panic
func onPanic(p *rt.Panic) {
	for {
	}
}

func main() {
	_, _, _, _ = ErrBad, root, cache, names
}
