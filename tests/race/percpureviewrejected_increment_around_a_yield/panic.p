package main

import "pluto/rt"

// corpusPanic is the panic handler a kernel build needs. The case's
// program is in main.p.
//
//pu:panic
func corpusPanic(p *rt.Panic) {
	for {
	}
}
