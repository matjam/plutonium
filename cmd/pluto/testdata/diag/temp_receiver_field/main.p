package main

// A pointer-receiver call needs an addressable operand (language spec
// section 11.14). A struct held directly by a map or returned by a call is a
// copy, so a call on its field would change a copy and is an error; the same
// field reached through a synced box is part of the shared object and is
// accepted (testdata/plutonium/boxtemprecv.p).

type C struct{ n int }

func (c *C) Inc() { c.n++ }

type W struct{ c C }

func mk() W { return W{} }

func main() {
	m := map[string]W{"a": W{}}
	m["a"].c.Inc()
	mk().c.Inc()
}
