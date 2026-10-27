// Translated by go2pu from internal/escape2/calls.go at the go-cutover tag; edited by hand since.

package escape2

import (
	"compiler/compat"
	"compiler/internal/escape2/contract"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/sched"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/moves"
	"compiler/internal/syntax/ast"
	"compiler/pu"
)

// Facts about calls for internal/moves (plan section (g), stage 4): for each
// argument expression of a call the program writes, whether the callee may
// keep a pointer to what the argument points to past the call (Self), may
// return pointers derived from it (Returned), and into which other
// arguments' memory it may store them (Into), as moves.ArgFacts states
// them. They are read off the final summaries of every function the call
// may reach, bound to the call's arguments as the solver binds them
// (solve/call.go, binding), so they say no less than the summaries the
// verdict was reached with.
//
// The drop x.Drop() is a call of x's drop glue with x's address, or none
// when x's type has nothing to destroy (eir.Program.NoCalls).
//
// An argument with no facts is one moves takes at its worst: one of a call
// the EIR does not have (in a function the program never reaches), or
// several values passed as one (f(g()) with several results).

// CallFacts returns the facts about the calls of the program p, which the
// analysis's result res is of.
func CallFacts(p *eir.Program, res *sched.Result) *moves.Calls {
	calls := moves.NewCalls()
	if p == nil || res == nil || res.Env == nil {
		return calls
	}
	env := res.Env
	ps := env.Paths()
	for _, e := range p.NoCalls {
		if se := funSelector(e.Fun); se != nil {
			calls.Add(se.X, &moves.ArgFacts{}) // a drop that runs nothing keeps nothing
		}
	}
	for _, f := range p.Funcs {
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op == eir.OpCall && in.Call != nil && in.Call.Expr != nil {
					noteCall(calls, env, ps, f, in.Call)
				}
			}
		}
	}
	return calls
}

// A callee is one function a call may reach, as the facts see it: its
// summary, or nil when nothing is known of it (a function without a body),
// and its number of inputs.
type callee struct {
	sum    *summary.Summary
	inputs int
}

// callees returns the functions call c of f may reach.
func callees(env solve.Env, f *eir.Func, c *eir.Call) []callee {
	prog := env.Program()
	one := func(id eir.FuncID) callee {
		if id == eir.NoFunc {
			return callee{inputs: len(c.Args)}
		}
		if int(id) < len(prog.Funcs) {
			fn := prog.Funcs[id]
			if fn.Bodyless {
				return callee{inputs: len(fn.Inputs)}
			}
			sum := env.Summary(id)
			if sum == nil {
				return callee{inputs: len(fn.Inputs)}
			}
			return callee{sum: sum, inputs: len(fn.Inputs)}
		}
		// A virtual function: its summary is all there is of it.
		sum := env.Summary(id)
		if sum == nil {
			return callee{inputs: len(c.Args)}
		}
		return callee{sum: sum, inputs: sum.Inputs}
	}
	switch c.Kind {
	case eir.CallStatic:
		return compat.Clone([]callee{one(c.Callee)})
	case eir.CallContract:
		if sum, ok := env.Contract(c.Contract); ok {
			return compat.Clone([]callee{{sum: sum, inputs: len(c.Args)}})
		}
		return compat.Clone([]callee{{inputs: len(c.Args)}})
	}
	var out []callee
	for _, id := range env.Candidates(f, c) {
		out = compat.Append(out, one(id))
	}
	return out
}

// A slot is an argument of a call as moves names it: the expressions its
// facts are recorded under (one, or the packed variadic arguments), and
// whether the callee's input is bound to the argument's own pointer
// (exact), so that a key of it that loads names pointers loaded through
// the argument rather than the argument's own.
type slot struct {
	exprs   []ast.Expr
	packed_ bool
	exact   bool
}

// slots returns, by the callee input each is bound to, the arguments of
// call c that moves names (solve/call.go, binding). A nil entry is an
// input bound to no argument expression.
func slots(c *eir.Call) []*slot {
	e := c.Expr
	var recv ast.Expr
	if se := funSelector(e.Fun); se != nil {
		recv = se.X
	}
	if c.Implicit == eir.ImplDrop {
		// x.Drop(): the drop glue of x's type, given x's address.
		if recv == nil {
			return nil
		}
		return compat.Clone([]*slot{pu.New(slot{exprs: compat.Clone([]ast.Expr{recv}), exact: true})})
	}
	var out []*slot
	// The receiver: Target for CallIface (the interface's data word,
	// whose relation to the method's receiver depends on the candidate),
	// Args[0] for a method call that prefixes it.
	prefixed := false
	switch c.Kind {
	case eir.CallIface:
		if recv != nil && c.Target != 0 {
			out = compat.Append(out, pu.New(slot{exprs: compat.Clone([]ast.Expr{recv})}))
		} else {
			out = compat.Append(out, nil)
		}
	case eir.CallTypeParam:
		prefixed = true
	case eir.CallStatic, eir.CallContract:
		prefixed = c.Sig != nil && c.Sig.Recv() != nil || c.Contract == contract.RingMethod
	}
	if prefixed {
		if recv != nil {
			// A type parameter's method may take the operand's address
			// or its value, by instance (RecvChoice): not exact.
			out = compat.Append(out, pu.New(slot{exprs: compat.Clone([]ast.Expr{recv}), exact: c.Kind != eir.CallTypeParam}))
		} else {
			out = compat.Append(out, nil)
		}
	}
	if c.Sig == nil {
		return out
	}
	n := c.Sig.Params().Len()
	if len(e.Args) == 1 && n > 1 {
		return out // f(g()) with several results
	}
	variadic := c.Sig.Variadic() && !e.Ellipsis.IsValid()
	for i := 0; i < n; i++ {
		switch {
		case variadic && i == n-1:
			if i < len(e.Args) {
				out = compat.Append(out, pu.New(slot{exprs: e.Args[i:], packed_: true}))
			} else {
				out = compat.Append(out, nil)
			}
		case i < len(e.Args):
			out = compat.Append(out, pu.New(slot{exprs: compat.Clone([]ast.Expr{e.Args[i]}), exact: true}))
		default:
			out = compat.Append(out, nil) // a default argument (section 11.6)
		}
	}
	return out
}

// funSelector returns the selector x.M that the function of a call names,
// with any type arguments (x.M[T]) removed, or nil (as internal/moves
// finds the receiver of a call).
func funSelector(fun ast.Expr) *ast.SelectorExpr {
	switch ix := ast.Unparen(fun).(type) {
	case *ast.IndexExpr:
		fun = ix.X
	case *ast.IndexListExpr:
		fun = ix.X
	}
	se, _ := ast.Unparen(fun).(*ast.SelectorExpr)
	return se
}

// noteCall records the facts about the arguments of call c of f.
func noteCall(calls *moves.Calls, env solve.Env, ps domain.Paths, f *eir.Func, c *eir.Call) {
	sl := slots(c)
	if len(sl) == 0 {
		return
	}
	// A dynamic call with no candidates never runs, and keeps nothing.
	cands := callees(env, f, c)
	for i, s := range sl {
		if s == nil {
			continue
		}
		af := &moves.ArgFacts{}
		for _, cd := range cands {
			merge(af, argFacts(ps, cd, sl, i))
		}
		for _, x := range s.exprs {
			calls.Add(x, af)
		}
	}
}

// merge adds what o says to af.
func merge(af, o *moves.ArgFacts) {
	af.Self = af.Self || o.Self
	af.Returned = af.Returned || o.Returned
	af.Into = compat.Append(af.Into, o.Into...)
}

// argFacts returns what callee cd's summary says of its input i, given the
// call's slots.
func argFacts(ps domain.Paths, cd callee, sl []*slot, i int) *moves.ArgFacts {
	af := pu.New(moves.ArgFacts{})
	if cd.sum == nil || i >= cd.inputs {
		// Nothing is known of the callee, or the input is one it does
		// not have (a candidate of another arity): the worst.
		af.Self, af.Returned = true, true
		af.Into = compat.Append(af.Into, moves.Into{})
		return af
	}
	s := sl[i]
	// A key of the input names the argument's own pointers when it loads
	// nothing; anything kept of a packed argument, or of an argument whose
	// input is not bound to it exactly, counts as the argument itself.
	self := func(k domain.Key) bool {
		return s.packed_ || !s.exact || ps.Loads(k.Path()) == 0 && !k.Deep()
	}
	sum := cd.sum
	for _, k := range sum.Esc {
		if k.Input() == i && self(k) {
			af.Self = true
		}
	}
	for _, st := range sum.Store {
		if st.Src.Input() != i {
			continue
		}
		if self(st.Src) {
			af.Self = true
		}
		var dst ast.Expr
		direct := false
		if j := st.Dst.Input(); j < len(sl) && sl[j] != nil && !sl[j].packed_ && len(sl[j].exprs) == 1 {
			dst = sl[j].exprs[0]
			direct = sl[j].exact && ps.Loads(st.Dst.Path()) == 0 && !st.Dst.Deep() && !st.Dst.GE()
		}
		af.Into = compat.Append(af.Into, moves.Into{Arg: dst, Direct: direct})
	}
	for _, r := range sum.Res {
		for _, rk := range r.From {
			if rk.Key.Input() != i {
				continue
			}
			af.Returned = true
			if self(rk.Key) {
				af.Self = true
			}
		}
	}
	// A call of a function value the callee was given, left to its
	// callers (decision D49): what it does with its arguments is not in
	// the summary. Anything it is passed of input i may be kept, and kept
	// where moves cannot follow. A function value loaded through input i
	// is not given i: it reaches only what its own record holds, which
	// moves takes as kept where the value was made (a literal's variables,
	// a method value's receiver, an address given to the call that made
	// it), so it keeps nothing of i that moves follows, though its results
	// may hold what its record does.
	for _, d := range sum.Deferred {
		fn := d.Fn.Input() == i
		passed := fn && self(d.Fn)
		af.Self = af.Self || passed
		for _, a := range d.Args {
			for _, k := range a.Keys {
				if k.Input() == i {
					passed = true
					af.Self = af.Self || self(k)
				}
			}
		}
		if passed {
			af.Into = compat.Append(af.Into, moves.Into{})
		}
		if fn || passed {
			af.Returned = true
		}
	}
	return af
}
