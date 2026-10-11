package main

// An or clause that panics with the error (language spec section 11.27):
// the trace names the line of the or statement in the function that has
// it, and the program exits as for any other panic.

import "pluto/errors"

var errNotFound = errors.New("not found")

func find(key string) (int, error) {
	if key == "" {
		return 0, errNotFound
	}
	return len(key), nil
}

func lookup(key string) int {
	n := find(key) or panic(err)
	return n
}

func main() {
	println(lookup("abc"))
	println(lookup(""))
}
