package opt

import (
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Notes on //pu:inline calls left out of line (pluto build --inlinenotes).
//
// //pu:inline asks for a function to be inlined whatever its size, but some
// calls still cannot be: recursive calls, calls on paths that panic, calls
// that would take the caller past its growth limit, and so on. The inliner
// records why it left each call to such a function where it was, and once
// the pipeline has run, InlineNotes lists the calls that are still there.

// maxInlineRounds is the number of rounds the inliner makes over a caller.
const maxInlineRounds = 8

// An InlineNote is a call to a //pu:inline function that was not inlined,
// or a use of such a function as a func value, through which calls are
// never inlined.
type InlineNote struct {
	Pos       token.Pos
	Callee    string // the function's name, as in stack traces
	Reason    string
	FuncValue bool // a func value of Callee rather than a call
}

// ReportInlining makes Run record why calls to //pu:inline functions were
// left out of line, for InlineNotes.
func (p *Pipeline) ReportInlining() {
	p.why = map[*ssa.Value]string{}
}

// InlineNotes returns, after Run, the calls to //pu:inline functions left
// in the program and the func values of such functions, in the order of
// their positions. ReportInlining must have been called before Run.
func (p *Pipeline) InlineNotes() []InlineNote { return p.notes }

// note records why the call v to g was not inlined, if g is marked
// //pu:inline and reasons are being recorded.
func note(why map[*ssa.Value]string, v *ssa.Value, g *ssa.Func, reason string) {
	if why != nil && g.Pragmas&ssa.PragmaInline != 0 {
		why[v] = reason
	}
}

// noteBlock records reason for every call in b to a //pu:inline function,
// or that the function never returns, for the call that ends a panic path.
func noteBlock(why map[*ssa.Value]string, b *ssa.Block, funcs map[*ssa.Sym]*ssa.Func, reason string) {
	if why == nil {
		return
	}
	for _, v := range b.Values {
		if g := callTarget(v, funcs); g != nil {
			if v.Call != nil && v.Call.NoReturn && !returns(g) {
				note(why, v, g, "it never returns")
			} else {
				note(why, v, g, reason)
			}
		}
	}
}

// notInlinable says why shouldInline rejects g, a //pu:inline function.
func notInlinable(g *ssa.Func) string {
	switch {
	case g.Pragmas&ssa.PragmaNoInline != 0:
		return "it is also marked //pu:noinline"
	case g.Pragmas&ssa.PragmaInterrupt != 0:
		return "it is an interrupt handler"
	case !returns(g):
		return "it never returns"
	}
	return "it is too large"
}

// collectInlineNotes lists the calls to //pu:inline functions in p and
// their func values. why holds the inliner's reasons, or is nil if the
// inliner did not run.
func collectInlineNotes(p *ssa.Program, why map[*ssa.Value]string, inlined bool) []InlineNote {
	funcs := map[*ssa.Sym]*ssa.Func{}
	for _, f := range p.Funcs {
		funcs[f.Sym] = f
	}
	// The static closure records of functions used as func values: a code
	// pointer alone.
	records := map[*ssa.Sym]*ssa.Func{}
	for _, d := range p.Data {
		if d.Size == 8 && len(d.Relocs) == 1 && d.Relocs[0].Off == 0 && d.Relocs[0].Addend == 0 {
			if g := funcs[d.Relocs[0].Sym]; g != nil {
				records[d.Sym] = g
			}
		}
	}
	type key struct {
		pos       token.Pos
		callee    *ssa.Func
		funcValue bool
	}
	seen := map[key]int{}
	var notes []InlineNote
	var reasons [][]string // of each note
	add := func(v *ssa.Value, g *ssa.Func, reason string, funcValue bool) {
		k := key{v.Pos, g, funcValue}
		i, ok := seen[k]
		if !ok {
			seen[k] = len(notes)
			notes = append(notes, InlineNote{Pos: v.Pos, Callee: g.Name, FuncValue: funcValue})
			reasons = append(reasons, []string{reason})
			return
		}
		// The same call, copied into other functions by inlining the
		// function it is in; it may have stayed for another reason there.
		for _, r := range reasons[i] {
			if r == reason {
				return
			}
		}
		reasons[i] = append(reasons[i], reason)
	}
	for _, f := range p.Funcs {
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				switch v.Op {
				case ssa.OpFuncAddr, ssa.OpSymAddr:
					// The code address, or a static closure record holding it.
					sym := v.Aux.(*ssa.Sym)
					g := funcs[sym]
					if v.Op == ssa.OpSymAddr {
						g = records[sym]
					}
					if g != nil && g.Pragmas&ssa.PragmaInline != 0 {
						add(v, g, "calls through a func value are not inlined", true)
					}
					continue
				}
				g := callTarget(v, funcs)
				if g == nil || g.Pragmas&ssa.PragmaInline == 0 || v.Call != nil && v.Call.Implicit {
					// Runtime calls the compiler made (string counts)
					// are not the program's to tune.
					continue
				}
				reason := why[v]
				switch {
				case !inlined:
					reason = "inlining runs only at -O2"
				case reason == "":
					// Made by inlining in the last round.
					reason = "the inliner stopped after its last round"
				}
				add(v, g, reason, false)
			}
		}
	}
	for i := range notes {
		notes[i].Reason = strings.Join(reasons[i], "; ")
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].Pos < notes[j].Pos })
	return notes
}
