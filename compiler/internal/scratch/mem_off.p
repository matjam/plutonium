//pu:build !gencheck

package scratch

import "pluto/alloc"

// The scratch memory of a level or a Worker is a bump (scratch.p): raw
// memory with no record of each allocation.
type mem = bump

type memMark = bumpMark

func newMem(parent alloc.Allocator) mem { return bump{parent: parent} }
