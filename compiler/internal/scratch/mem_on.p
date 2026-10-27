//pu:build gencheck

package scratch

import (
	"pluto/alloc"
	"pluto/arena"
)

// With generation checks the scratch memory is pluto/arena's Chunked, which
// tags its allocations and invalidates them when it releases them. A bump
// would be an ordinary custom allocator there: each allocation registered
// on its own and never unregistered, as Release frees no allocation one by
// one, so the registrations of a chunk piled up and every lookup in it
// walked them.
type mem = arena.Chunked

type memMark = arena.ChunkMark

func newMem(parent alloc.Allocator) mem { return arena.NewChunked(chunkSize, parent) }
