package escape

// The record of the calls the analysis applies, for the lowering oracle (plan
// docs/plans/2026-10-20-escape-redesign.md, section 3.1): every function
// whose summary the reporting pass applies at a call, by the call's position,
// and every candidate of a dynamic call that a filter left out. The oracle
// compares it with the calls the generated code makes
// (internal/lower.CallFacts).
//
// This file is not translated by tools/go2pu (its untranslated map): it is
// test machinery of the Go compiler.

import (
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Callee is a function the analysis applied at a call.
type Callee struct {
	Obj   *types2.Func   // a declared function or method, by origin
	Lit   *ast.FuncLit   // a function literal
	Range *ast.RangeStmt // the body of a range loop over a function
	// Unknown: a function the analysis cannot see, whose call it treats as
	// keeping every pointer passed (always sound).
	Unknown bool
	// Bodyless: applied by a summary the analysis made up (a function with no
	// body, or one the lowerer expands inline: intrinsics.go).
	Bodyless bool
	Name     string
}

// Skip is a candidate of a dynamic call that the analysis left out, and why.
type Skip struct {
	Callee
	Why string
}

// CallLog is what the reporting pass applied, by call position.
type CallLog struct {
	Applied map[token.Pos][]Callee
	Skipped map[token.Pos][]Skip
	// Walked are the positions of the functions the reporting pass walked:
	// declarations, literals and range bodies, by their first position.
	Walked map[token.Pos]bool
}

// AnalyzeLogged runs the analysis as Analyze does and also returns the record
// of the calls the reporting pass applied. It does not stop reporting after
// the usual number of errors, so that every function is walked.
func AnalyzeLogged(prog *load.Program) (*Calls, *CallLog, error) {
	a := newAnalyzer(prog, false)
	a.calls = &Calls{args: map[ast.Expr]*ArgFacts{}}
	log := &CallLog{Applied: map[token.Pos][]Callee{}, Skipped: map[token.Pos][]Skip{}, Walked: map[token.Pos]bool{}}
	ranges := map[*function]*ast.RangeStmt{}
	callee := func(f *function) Callee {
		if f == nil {
			return Callee{Unknown: true, Name: "an unknown function"}
		}
		c := Callee{Obj: f.obj, Lit: f.lit, Bodyless: f.bodyless, Name: f.name}
		if f.obj != nil {
			c.Obj = f.obj.Origin()
		}
		if f.loopBody {
			if ranges[f] == nil {
				for st, b := range a.bodies {
					ranges[b] = st
				}
			}
			c.Range = ranges[f]
		}
		return c
	}
	a.oracle = func(f *function, pos token.Pos, skipped string) {
		c := callee(f)
		if skipped != "" {
			log.Skipped[pos] = append(log.Skipped[pos], Skip{c, skipped})
			return
		}
		for _, d := range log.Applied[pos] {
			if d == c {
				return
			}
		}
		log.Applied[pos] = append(log.Applied[pos], c)
	}
	err := a.finish()
	for _, f := range a.order {
		if f.decl != nil && f.decl.Body != nil {
			log.Walked[f.decl.Pos()] = true
		}
	}
	for lit := range a.lits {
		log.Walked[lit.Pos()] = true
	}
	for st := range a.bodies {
		log.Walked[st.Pos()] = true
	}
	return a.calls, log, err
}

// A Keeper is a function that may be reached through a dynamic call (a
// method of a type converted to an interface, or a function used as a func
// value) and keeps a pointer it receives as a parameter: the implementations
// the provisional rule for dynamic calls (decision D47) would reject.
type Keeper struct {
	Func  string
	Pos   token.Position
	Via   string // "method of a converted type" or "func value"
	Param string // the parameter's name, "receiver" for the receiver
	// How: "escapes" (heap, package memory, channel, task), "stored into
	// <param>" (memory another parameter points to), "stored into itself",
	// "returned".
	How string
	// Own: the parameter's own pointer is kept (no load on the path);
	// otherwise a pointer loaded through it.
	Own bool
}

// DynamicKeepers runs the analysis and lists the keepers among the functions
// a dynamic call may reach, and the number of those functions. Parameters of owner, counted, map and channel
// types are left out: the rule does not cover them.
//
// reached, if not nil, says which functions a dynamic call of the program
// may reach (from the lowering oracle's targets of the calls through
// interfaces and func values); the others are left out.
func DynamicKeepers(prog *load.Program, reached func(Callee) bool) ([]Keeper, int, error) {
	a := newAnalyzer(prog, false)
	err := a.finish()
	type cand struct {
		f   *function
		via string
	}
	var cands []cand
	seen := map[*function]bool{}
	for _, ms := range a.convMethods {
		for _, m := range ms {
			if !seen[m.c.f] {
				seen[m.c.f] = true
				cands = append(cands, cand{m.c.f, "method of a converted type"})
			}
		}
	}
	for fv := range a.funcValues {
		if !seen[fv.f] {
			seen[fv.f] = true
			cands = append(cands, cand{fv.f, "func value"})
		}
	}
	var out []Keeper
	for _, c := range cands {
		f := c.f
		if f.bodyless || f.loopBody {
			continue
		}
		if reached != nil {
			c := Callee{Obj: f.obj, Lit: f.lit}
			if f.obj != nil {
				c.Obj = f.obj.Origin()
			}
			if !reached(c) {
				continue
			}
		}
		covered := func(idx int) bool {
			if idx >= len(f.params) {
				return false
			}
			T := f.params[idx].Type()
			return hasPointers(T) && !isCounted(T) && !isChan(T) && !types2.IsOwner(T)
		}
		name := func(idx int) string {
			if idx == 0 && f.sig.Recv() != nil {
				return "receiver"
			}
			return f.paramName(idx)
		}
		var pos token.Pos
		switch {
		case f.decl != nil:
			pos = f.decl.Pos()
		case f.lit != nil:
			pos = f.lit.Pos()
		}
		add := func(k pkey, how string) {
			if !covered(k.idx) || k.counts {
				return
			}
			out = append(out, Keeper{Func: f.name, Pos: a.fset.Position(pos), Via: c.via, Param: name(k.idx), How: how, Own: selfKey(k) && !k.ge})
		}
		for _, k := range f.sum.sortedEscapes() {
			add(k, "escapes")
		}
		for _, e := range f.sum.sortedStores() {
			if e.src.idx == e.dst.idx {
				add(e.src, "stored into itself")
			} else {
				add(e.src, "stored into "+name(e.dst.idx))
			}
		}
		for _, r := range f.sum.results {
			for _, k := range r.sortedFrom() {
				add(k.pkey, "returned")
			}
		}
	}
	return out, len(cands), err
}
