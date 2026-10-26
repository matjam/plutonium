package backend

import (
	"selfhost/internal/scratch"
	"selfhost/internal/ssa"
)

// emitAll is the Go compiler's emitAll (internal/backend/par.go), run in
// order on one task, each function in scratch memory. The Go version
// compiles batches in parallel; here the tasks would write memory of one
// type through views the race check cannot tell apart (spec section 19.4,
// Memory reached through views), so it stays sequential. Hand-written;
// go2pu keeps it in place of a translation of par.go.
func (e *emitter) emitAll(funcs []*ssa.Func) error {
	for _, f := range funcs {
		e.f = f
		scratch.Do(e)
		if e.err != nil {
			return e.err
		}
	}
	return nil
}
