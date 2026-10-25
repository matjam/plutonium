// Package scratch is the self-hosted compiler's internal/scratch: Do runs
// a step with an arena as the default allocator and frees the arena's
// memory when the step's results have been copied out. Hand-written; go2pu
// keeps it in place of a translation of scratch.go.
package scratch

import (
	"pluto/alloc"
	"pluto/arena"
	"selfhost/compat"
)

// chunkSize is the size of the arena's chunks. Released chunks of this size
// stay with the arena for the next step; larger ones go back to the heap.
const chunkSize = 1 << 20

var (
	scr   arena.Chunked
	ready bool
	busy  bool // a Do's work is running
)

// A Step is a step of the compiler run by Do. Its methods' names are
// unique to it: the goexit analysis (internal/lower/goexit.go) takes an
// interface call to reach every method of its name and signature, and a
// call through a func() value to reach every func() literal, so Step keeps
// the step's code from widening either set.
type Step interface {
	// ScratchWork does the step's work, allocating from scratch memory.
	ScratchWork()
	// ScratchKeep copies out of scratch memory what ScratchWork made that
	// must outlive the step, allocating from the caller's allocator.
	ScratchKeep()
}

// Do runs s.ScratchWork with the arena as the default allocator, then
// s.ScratchKeep with the caller's, and then frees everything ScratchWork
// allocated. Do does not nest.
func Do(s Step) {
	if busy {
		panic("scratch.Do within another Do's work")
	}
	if !ready {
		scr = arena.NewChunked(chunkSize, alloc.Default())
		ready = true
	}
	busy = true
	m := scr.Mark()
	r := alloc.Use(&scr)
	s.ScratchWork()
	r.Restore()
	busy = false
	s.ScratchKeep()
	scr.Release(m)
}

// String returns a copy of s from the current allocator.
func String(s string) string { return compat.Concat(s) }

// Error returns an error with err's text whose memory comes from the
// current allocator, or nil.
func Error(err error) error {
	if err == nil {
		return nil
	}
	return compat.NewError(String(err.Error()))
}
