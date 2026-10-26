// Package scratch runs a step of the compiler whose allocations die with
// it, such as generating one function's code (self-hosting plan section
// 3.4).
//
// In the self-hosted compiler, Do runs the step with an arena as the
// default allocator and frees the arena's memory afterwards, so the
// compiler's memory stays bounded by its largest step instead of growing
// with the program. Here Go's collector frees what the step leaves, so Do
// only calls its methods and the copies are plain copies. The hand-written
// selfhost/internal/scratch/scratch.p replaces this file there.
package scratch

import (
	"errors"
	"strings"
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

// Do runs s.ScratchWork, then s.ScratchKeep, and then frees what
// ScratchWork allocated. ScratchKeep copies text out with String and Error.
// Nothing ScratchWork allocated may be reachable from anything else after
// Do returns. Do nests, up to three deep: the inner step's ScratchKeep
// copies into the outer step's memory.
//
// Builds here may run on several goroutines at once, so Do keeps no state
// and cannot check how deeply it nests; the self-hosted Do checks.
func Do(s Step) {
	s.ScratchWork()
	s.ScratchKeep()
}

// String returns a copy of s from the current allocator.
func String(s string) string { return strings.Clone(s) }

// Error returns an error with err's text whose memory comes from the
// current allocator, or nil.
func Error(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(String(err.Error()))
}

// Trim returns to the heap the spare memory the self-hosted arenas keep
// for later steps, after a large step whose like does not follow. Here it
// does nothing.
func Trim() {}

// Used returns how much of the current step's scratch memory is in use.
// Here there is no scratch memory, so it is always 0.
func Used() int { return 0 }

// Outer makes the allocator that was current when Do began the current one
// until Restore is called, so that memory a step adds to state outliving it
// (a memo table, an interning table, a pool) does not come from scratch
// memory. Outside a Do's work it changes nothing. Here, where Go's
// collector frees, it does nothing:
//
//	defer scratch.Outer().Restore()
func Outer() Restorer { return Restorer{} }

// Heap makes the heap the current allocator until Restore is called, so
// that a cache filled lazily by whichever step first asks (the type
// checker's expanded methods and underlying types) does not come from any
// step's scratch memory. Here it does nothing.
func Heap() Restorer { return Restorer{} }

// A Restorer puts back the allocator Outer replaced.
type Restorer struct{}

// Restore puts back the allocator Outer replaced.
func (Restorer) Restore() {}
