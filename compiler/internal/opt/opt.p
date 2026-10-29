// Translated by go2pu from internal/opt/opt.go at the go-cutover tag; edited by hand since.

// Package opt is Plutonium's SSA optimiser: a pipeline of passes run over
// every function of a program.
package opt

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
	"pluto/time"
)

// A Pass transforms one function.
type Pass struct {
	Name string
	Run  func(f *ssa.Func)
}

// A ProgramPass transforms the whole program (for example, inlining).
type ProgramPass struct {
	Name string
	Run  func(p *ssa.Program)
}

// A Pipeline is an ordered list of passes. Program passes run between the
// function passes listed before and after them.
type Pipeline struct {
	steps []step

	// For InlineNotes (inlinenotes.go): the inliner's reasons, set by
	// ReportInlining, whether it ran, and the notes Run collected.
	why     map[*ssa.Value]string
	inlined bool
	notes   []InlineNote

	// OnStep, if set, is called with the name of each step as it starts:
	// a program pass, or the first of a run of function passes.
	OnStep func(name string)

	// OnTime, if set, is called after each step with the name of each of
	// its passes and the time the pass took over every function.
	OnTime func(name string, d time.Duration)
}

type step struct {
	fn   *Pass
	prog *ProgramPass
	// inline marks the inliner's step, which Run gives the pipeline's
	// record of reasons for InlineNotes.
	inline bool
}

// ForLevel returns the passes for an optimisation level.
func ForLevel(level int) *Pipeline {
	p := pu.New(Pipeline{})
	if level >= 1 && compat.Getenv("PLUTO_NOSTRELIDE") == "" {
		// Before anything inlines the runtime's count calls.
		// PLUTO_NOSTRELIDE turns it off, to measure what it saves.
		p.steps = compat.Append(p.steps, step{prog: pu.New(ProgramPass{"strelide", ElideStringHolds})})
	}
	p.add(Pass{"copyelim", ssa.CopyElim})
	p.add(Pass{"deadcode", ssa.DeadCode})
	if level >= 1 {
		p.add(Pass{"sroa", SROA})
		p.add(Pass{"mem2reg", Mem2Reg})
		p.add(Pass{"simplify", Simplify})
		p.add(Pass{"simplifycfg", SimplifyCFG})
	}
	if level == 1 {
		p.add(Pass{"runebounds", RuneBounds})
	}
	if level >= 2 {
		p.add(Pass{"cse", CSE})
		// After CSE, which makes the lengths and offsets it compares one
		// value each, and before inlining, which hides the calls it
		// recognises.
		p.add(Pass{"runebounds", RuneBounds})
		p.add(Pass{"schedule", ssa.Schedule})
		// Before inlining, so that copies inlined into callers carry the
		// loop rather than the recursion.
		p.add(Pass{"tailrec", TailRec})
		p.steps = compat.Append(p.steps, step{prog: pu.New(ProgramPass{"inline", Inline}), inline: true})
		p.add(Pass{"prove", Prove})
		p.add(Pass{"simplify2", Simplify})
		// After prove, which removes the bounds checks that would give a
		// loop more than one exit.
		p.add(Pass{"unroll", Unroll})
		p.add(Pass{"branchelim", BranchElim})
		p.add(Pass{"simplifycfg2", SimplifyCFG})
		// Simplify the selects branchelim made.
		p.add(Pass{"simplify3", Simplify})
		// After branchelim, which turns || and && chains into Or and And.
		p.add(Pass{"bittest", BitTest})
		p.add(Pass{"memopt", MemOpt})
		// Constant indexes folded since the first SROA, and accesses
		// memopt removed, can make more slots splittable.
		p.add(Pass{"sroa2", SROA})
		p.add(Pass{"mem2reg2", Mem2Reg})
		p.add(Pass{"simplify4", Simplify})
		// Branches on what memopt found constant (a slot's cleared
		// bytes, seen past calls) go, with the calls they guarded.
		p.add(Pass{"simplifycfg3", SimplifyCFG})
		p.add(Pass{"cse2", CSE})
		p.add(Pass{"loopidiom", LoopIdiom})
		p.add(Pass{"vectorize", Vectorize})
		p.add(Pass{"deadcode2", ssa.DeadCode})
	}
	// Last, so that inlining and scalar replacement see the code without
	// them; a no-op unless the build asked for generation checks. GenFrees
	// finds the calls that cannot free, which checks may stay valid across.
	p.steps = compat.Append(p.steps, step{prog: pu.New(ProgramPass{"genfrees", GenFrees})})
	p.add(Pass{"gencheck", GenCheck})
	return p
}

func (p *Pipeline) add(gopass Pass) {
	pass := pu.New(gopass)
	p.steps = compat.Append(p.steps, step{fn: pass})
}

// checkSSA, when set by the PLUTO_SSACHECK environment variable, verifies
// every function after every pass.
var checkSSA = compat.Getenv("PLUTO_SSACHECK") != ""

// verify checks f's invariants and its source positions.
func verify(f *ssa.Func) error {
	f.Verify() or return
	return f.VerifyPos()
}

// Run applies the pipeline. after, if non-nil, is called after each
// function pass with the pass name.
func (p *Pipeline) Run(prog *ssa.Program, after func(pass string, f *ssa.Func)) error {
	if checkSSA {
		for _, f := range prog.Funcs {
			if err := verify(f); err != nil {
				return compat.Wrap(compat.Sprintfb("before optimization: %w", compat.ArgW(err)), err)
			}
		}
	}
	for i := 0; i < len(p.steps); i++ {
		st := p.steps[i]
		if st.fn != nil && after == nil && !checkSSA {
			// A run of function passes, each function through all of
			// them, the functions in parallel (par.go).
			var group []*Pass
			for ; i < len(p.steps) && p.steps[i].fn != nil; i++ {
				group = compat.Append(group, p.steps[i].fn)
			}
			i--
			if p.OnStep != nil {
				p.OnStep(group[0].Name)
			}
			// The inline notes (p.why) name calls by their values, which
			// copying the functions out (par.go) would change.
			var times []time.Duration
			if p.OnTime != nil {
				times = compat.Make[time.Duration](len(group))
			}
			runFuncs(prog.Funcs, group, times, p.why == nil)
			for j, d := range times {
				p.OnTime(group[j].Name, d)
			}
			continue
		}
		if st.prog != nil {
			if p.OnStep != nil {
				p.OnStep(st.prog.Name)
			}
			var start time.Time
			if p.OnTime != nil {
				start = time.Now()
			}
			if st.inline {
				inline(prog, p.why)
				p.inlined = true
			} else {
				st.prog.Run(prog)
			}
			if p.OnTime != nil {
				p.OnTime(st.prog.Name, time.Since(start))
			}
			if checkSSA {
				for _, f := range prog.Funcs {
					if err := verify(f); err != nil {
						return compat.Wrap(compat.Sprintfb("after %s: %w", compat.ArgS(st.prog.Name), compat.ArgW(err)), err)
					}
				}
			}
			continue
		}
		for _, f := range prog.Funcs {
			st.fn.Run(f)
			if checkSSA {
				if err := verify(f); err != nil {
					return compat.Wrap(compat.Sprintfb("after %s: %w", compat.ArgS(st.fn.Name), compat.ArgW(err)), err)
				}
			}
			if after != nil {
				after(st.fn.Name, f)
			}
		}
	}
	if p.why != nil {
		p.notes = collectInlineNotes(prog, p.why, p.inlined)
		if p.inlined {
			detachFuncs(prog.Funcs)
		}
	}
	return nil
}
