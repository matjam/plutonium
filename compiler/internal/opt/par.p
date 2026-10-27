package opt

import (
	"compiler/internal/scratch"
	"compiler/internal/ssa"
	"unsafe"
)

// runFuncs is the Go compiler's runFuncs (internal/opt/par.go), run in
// order on one task. The Go version runs the functions in parallel, each
// writing its own *ssa.Func; here the tasks would write memory of one
// type through views the race check cannot tell apart (spec section 19.4,
// Memory reached through views), so it stays sequential until the
// functions are owned by the tasks that change them.
//
// Each function runs through the passes in scratch memory and is then
// copied out of it (ssa.Copier) into memory of its own, which replaces the
// function's previous copy (scratch.DoHome), so what the passes allocated
// and dropped, and the code they replaced, go with the step instead of
// piling up for the rest of the build. With
// copyOut false, when something outside the functions holds on to their
// values (the inline notes), they run in place. Hand-written; go2pu keeps
// it in place of a translation of par.go.
func runFuncs(funcs []*ssa.Func, passes []*Pass, copyOut bool) {
	if !copyOut {
		for _, f := range funcs {
			for _, ps := range passes {
				ps.Run(f)
			}
		}
		return
	}
	var s funcStep
	s.passes = passes
	for _, f := range funcs {
		s.f = f
		scratch.DoHome(unsafe.Pointer(f), &s)
	}
	scratch.Trim()
}

// funcStep runs one function through passes as a scratch step.
type funcStep struct {
	f      *ssa.Func
	passes []*Pass
	cp     *ssa.Copier // made in the step's memory, for ScratchKeep
}

func (s *funcStep) ScratchWork() {
	used := scratch.Used()
	for _, ps := range s.passes {
		ps.Run(s.f)
	}
	// Passes that allocated nothing (gencheck in a build without checks)
	// left nothing in scratch memory, and copying would only duplicate f.
	if scratch.Used() != used {
		s.cp = ssa.NewCopier(s.f)
	}
}

func (s *funcStep) ScratchKeep() {
	if s.cp != nil {
		s.cp.Copy(s.f)
		s.cp = nil
	}
}

// detachFuncs copies each function's code into fresh memory once the inline
// notes have been collected. With the notes, the inliner and the passes
// after it run in place (runFuncs with copyOut false), so a caller keeps
// pointers into the memory of the functions inlined into it: their tuple
// types, call descriptions and slot layouts. The backend frees each
// function's memory once it is emitted (scratch.Drop), which would leave
// a caller emitted later pointing into freed memory. No function's memory
// is freed here, so the order of the copies does not matter.
func detachFuncs(funcs []*ssa.Func) {
	var s detachStep
	for _, f := range funcs {
		s.f = f
		scratch.Do(&s)
	}
	scratch.Trim()
}

// detachStep copies one function out as a scratch step: the copier's
// tables are made in scratch memory, the copy in the caller's.
type detachStep struct {
	f  *ssa.Func
	cp *ssa.Copier
}

func (s *detachStep) ScratchWork() { s.cp = ssa.NewCopier(s.f) }

func (s *detachStep) ScratchKeep() {
	s.cp.Copy(s.f)
	s.cp = nil
}
