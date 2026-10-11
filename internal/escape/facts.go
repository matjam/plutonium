package escape

import (
	"strings"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/ast"
)

// Calls is what the analysis found about the arguments of calls, for the
// move checker: which views a call keeps past its return, and where. A
// view of a local owner that no call keeps lasts only for the call, so the
// owner's variable can be followed along the control flow as if its
// address had not been taken (language spec sections 18.3 and 18.6).
type Calls struct {
	args map[ast.Expr]*ArgFacts
}

// ArgFacts says what a call does with the pointers in one argument, given
// by its expression (for a method call, the receiver expression x of
// x.M(...), whose address the call takes if the method has a pointer
// receiver).
type ArgFacts struct {
	// Self: the callee may keep a pointer to the memory the argument
	// points to (not merely to memory loaded through it) past the call:
	// it stores it somewhere, or returns it.
	Self bool
	// Returned: a result may hold pointers derived from the argument.
	Returned bool
	// Into are the arguments into whose memory the callee may store
	// pointers derived from this one. A nil entry is a destination that
	// is not an argument expression of the call (unknown).
	Into []Into
}

// Into is a destination of a store: the argument expression, and whether
// the pointers land in the memory it points to itself (Direct) rather than
// in memory loaded through it.
type Into struct {
	Arg    ast.Expr
	Direct bool
}

// Arg returns what the analysis knows of the call argument e, or nil if
// nothing is known (assume the worst).
func (c *Calls) Arg(e ast.Expr) *ArgFacts {
	if c == nil {
		return nil
	}
	return c.args[e]
}

// Analyze runs the analysis and returns what it found about calls, and the
// escapes as a diag.List, or nil.
func Analyze(prog *load.Program) (*Calls, error) {
	a := newAnalyzer(prog, false)
	a.calls = &Calls{args: map[ast.Expr]*ArgFacts{}}
	err := a.finish()
	return a.calls, err
}

// selfKey reports whether pointers named by k are the parameter's own
// (found without a load), as opposed to pointers loaded through it.
func selfKey(k pkey) bool { return !strings.Contains(k.path, "*") }

// noteCall records what f's summary says about the arguments of a call,
// in the reporting pass. f nil is a function the analysis cannot see.
func (a *analyzer) noteCall(f *function, cs *callSite, mode recvMode) {
	if !a.report || a.calls == nil || cs.keys == nil {
		return
	}
	for i, key := range cs.keys {
		var exprs []ast.Expr
		if key != nil {
			exprs = []ast.Expr{key}
		} else if i == cs.packedAt && cs.packed != nil {
			exprs = cs.packed
		}
		for _, e := range exprs {
			if a.calls.args[e] == nil {
				a.calls.args[e] = &ArgFacts{}
			}
			a.calls.args[e].merge(a.argFacts(f, cs, mode, i, key == nil))
		}
	}
}

// merge adds what o says to af.
func (af *ArgFacts) merge(o *ArgFacts) {
	af.Self = af.Self || o.Self
	af.Returned = af.Returned || o.Returned
	af.Into = append(af.Into, o.Into...)
}

// argFacts returns what f's summary says about parameter i of a call.
// packed says the argument is one of several packed into the parameter's
// variadic slice, where anything kept counts as the argument itself.
func (a *analyzer) argFacts(f *function, cs *callSite, mode recvMode, i int, packed bool) *ArgFacts {
	af := &ArgFacts{}
	if f == nil {
		af.Self, af.Returned = true, true
		af.Into = append(af.Into, Into{})
		return af
	}
	// Through an interface whose data word is not the receiver, any fact
	// about the receiver is about its own memory.
	self := func(k pkey) bool { return packed || selfKey(k) || k.idx == 0 && mode != recvExact }
	for k := range f.sum.escapes {
		if k.idx == i && self(k) {
			af.Self = true
		}
	}
	for e := range f.sum.stores {
		if e.src.idx != i {
			continue
		}
		if self(e.src) {
			af.Self = true
		}
		var dst ast.Expr
		if e.dst.idx < len(cs.keys) {
			dst = cs.keys[e.dst.idx]
		}
		af.Into = append(af.Into, Into{Arg: dst, Direct: dst != nil && selfKey(e.dst) && (e.dst.idx != 0 || mode == recvExact)})
	}
	for _, r := range f.sum.results {
		for k := range r.from {
			if k.idx != i {
				continue
			}
			af.Returned = true
			if self(k.pkey) {
				af.Self = true
			}
		}
	}
	return af
}
