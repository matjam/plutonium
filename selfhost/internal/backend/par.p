package backend

import (
	"selfhost/internal/scratch"
	"selfhost/internal/ssa"
	"unsafe"
)

// emitAll is the Go compiler's emitAll (internal/backend/par.go), run in
// order on one task, each function in scratch memory, freeing each
// function's code once it is written. The Go version compiles batches in
// parallel; here the tasks would write memory of one type through views
// the race check cannot tell apart (spec section 19.4, Memory reached
// through views), so it stays sequential. Hand-written; go2pu keeps it in
// place of a translation of par.go.
func (e *emitter) emitAll(funcs []*ssa.Func) error {
	for _, f := range funcs {
		e.f = f
		scratch.Do(e)
		if e.err != nil {
			return e.err
		}
		// The object and debug information keep copies of what they
		// need, so the function's optimised code can go.
		scratch.Drop(unsafe.Pointer(f))
	}
	return nil
}
