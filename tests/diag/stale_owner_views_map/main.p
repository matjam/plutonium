package main

import "pluto/hash"

// A pointer into a hash map is a view of the memory the map holds (language
// spec section 18.3): it may not be used after the map moved, was dropped
// or was given a new value.

func takeMap(m hash.Map[string, int]) {}
func verbose() bool                   { return true }

func main() {
	// A pointer into a hash map, after the map moved on some paths.
	m1 := hash.NewMap[string, int](4)
	m1.Set("a", 1)
	p1 := m1.Ptr("a")
	if verbose() {
		takeMap(m1)
	}
	_ = *p1

	// After an explicit drop.
	m2 := hash.NewMap[string, int](4)
	m2.Set("b", 2)
	p2 := m2.Ptr("b")
	m2.Drop()
	_ = *p2
}
