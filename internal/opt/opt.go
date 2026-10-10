// Package opt is Plutonium's SSA optimiser: a pipeline of passes run over
// every function of a program.
package opt

import (
	"fmt"
	"os"

	"github.com/matjam/plutonium/internal/ssa"
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
}

type step struct {
	fn   *Pass
	prog *ProgramPass
}

// ForLevel returns the passes for an optimisation level.
func ForLevel(level int) *Pipeline {
	p := &Pipeline{}
	p.add(Pass{"copyelim", ssa.CopyElim})
	p.add(Pass{"deadcode", ssa.DeadCode})
	if level >= 1 {
		p.add(Pass{"sroa", SROA})
		p.add(Pass{"mem2reg", Mem2Reg})
		p.add(Pass{"simplify", Simplify})
		p.add(Pass{"simplifycfg", SimplifyCFG})
	}
	if level >= 2 {
		p.add(Pass{"cse", CSE})
		p.add(Pass{"schedule", ssa.Schedule})
		p.addProgram(ProgramPass{"inline", Inline})
		p.add(Pass{"simplify2", Simplify})
		p.add(Pass{"prove", Prove})
		p.add(Pass{"simplifycfg2", SimplifyCFG})
		p.add(Pass{"deadcode2", ssa.DeadCode})
	}
	return p
}

func (p *Pipeline) add(pass Pass) { p.steps = append(p.steps, step{fn: &pass}) }

func (p *Pipeline) addProgram(pass ProgramPass) { p.steps = append(p.steps, step{prog: &pass}) }

// checkSSA, when set by the PLUTO_SSACHECK environment variable, verifies
// every function after every pass.
var checkSSA = os.Getenv("PLUTO_SSACHECK") != ""

// Run applies the pipeline. after, if non-nil, is called after each
// function pass with the pass name.
func (p *Pipeline) Run(prog *ssa.Program, after func(pass string, f *ssa.Func)) error {
	for _, st := range p.steps {
		if st.prog != nil {
			st.prog.Run(prog)
			if checkSSA {
				for _, f := range prog.Funcs {
					if err := f.Verify(); err != nil {
						return fmt.Errorf("after %s: %w", st.prog.Name, err)
					}
				}
			}
			continue
		}
		for _, f := range prog.Funcs {
			st.fn.Run(f)
			if checkSSA {
				if err := f.Verify(); err != nil {
					return fmt.Errorf("after %s: %w", st.fn.Name, err)
				}
			}
			if after != nil {
				after(st.fn.Name, f)
			}
		}
	}
	return nil
}
