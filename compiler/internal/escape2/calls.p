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
	"compiler/internal/types2"
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
				if fn.NoEscape {
					return callee{sum: noEscapeFacts(fn), inputs: len(fn.Inputs)}
				}
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

// noEscapeFacts is what //pu:noescape states of bodyless function f, as
// the solver takes it (solve/call.go, noEscapeSummary): it keeps no input,
// except by storing one where another points, and its results may hold
// what the inputs point to. A store into an input that can hold no pointer
// (atomic.Xchg32's new value) is left out.
func noEscapeFacts(f *eir.Func) *summary.Summary {
	n := len(f.Inputs)
	sum := pu.New(summary.Summary{Inputs: n})
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i != j && !pointerFree(f.Inputs[j].Type) {
				sum.Store = compat.Append(sum.Store, summary.StorePair{
					Src: domain.MakeKey(i, domain.Root, domain.KeyFlags{GE: true}),
					Dst: domain.MakeKey(j, domain.Root, domain.KeyFlags{GE: true}),
				})
			}
		}
	}
	if f.Results > 0 {
		sum.Res = compat.Make[summary.Result](f.Results)
	}
	for r := range sum.Res {
		for i := 0; i < n; i++ {
			deep := domain.MakeKey(i, domain.Root, domain.KeyFlags{Deep: true})
			sum.Res[r].From = compat.Append(sum.Res[r].From, summary.ResultKey{Key: deep, Field: domain.Root})
		}
	}
	return sum
}

// pointerFree reports whether a value of type T holds no pointer: a
// number or a boolean.
func pointerFree(T types2.Type) bool {
	if T == nil {
		return false
	}
	b, ok := T.Underlying().(*types2.Basic)
	return ok && b.Info()&(types2.IsNumeric|types2.IsBoolean) != 0
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

// A memberEnv gives the functions a dynamic call may reach, not joined
// into a virtual function (sched's env, Members).
type memberEnv interface {
	Members(f *eir.Func, c *eir.Call) []eir.FuncID
}

// noteCallees records, for a call the program writes that does not name a
// declared function, the functions it may reach (moves.Callee), for the
// move checker's summaries of what functions keep (moves/keeps.p).
func noteCallees(calls *moves.Calls, env solve.Env, f *eir.Func, c *eir.Call) {
	if c.Implicit != eir.ImplNone && c.Implicit != eir.ImplDefer {
		return
	}
	prog := env.Program()
	var ids []eir.FuncID
	switch c.Kind {
	case eir.CallStatic:
		if int(c.Callee) < len(prog.Funcs) && prog.Funcs[c.Callee].Key.Obj != nil {
			return // moves finds it from the source
		}
		ids = compat.Clone([]eir.FuncID{c.Callee})
	case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
		if c.Kind != eir.CallFunc && c.Method == nil {
			return // a drop
		}
		me, ok := env.(memberEnv)
		if !ok {
			calls.AddCallees(c.Expr, compat.Clone([]moves.Callee{{Unknown: true}}))
			return
		}
		ids = me.Members(f, c)
	default:
		return
	}
	out := compat.MakeCap[moves.Callee](0, len(ids))
	for _, id := range ids {
		out = compat.Append(out, moveCallee(prog, id))
	}
	calls.AddCallees(c.Expr, out)
}

// moveCallee names the function id as moves.Callee does.
func moveCallee(prog *eir.Program, id eir.FuncID) moves.Callee {
	if id == eir.NoFunc || int(id) >= len(prog.Funcs) {
		return moves.Callee{Unknown: true}
	}
	fn := prog.Funcs[id]
	if fn.Bodyless {
		return moves.Callee{} // no body: moves has no summary of it
	}
	k := fn.Key
	switch {
	case k.Obj != nil:
		return moves.Callee{Fn: k.Obj.Origin()}
	case k.Lit != nil:
		return moves.Callee{Lit: k.Lit}
	case k.Range != nil:
		return moves.Callee{Range: k.Range}
	case k.Synth != nil:
		m := k.Synth.Method
		switch k.Synth.Kind {
		case eir.SynthItabAdapter:
			// Method is the interface's: the adapter calls the method of
			// that name on Recv (eir/synth.go, itabAdapterBody).
			if m != nil && k.Synth.Recv != nil {
				obj, _, _ := types2.LookupFieldOrMethod(k.Synth.Recv, false, m.Pkg(), m.Name())
				if fn, ok := obj.(*types2.Func); ok && declared(fn) {
					return moves.Callee{Fn: fn.Origin()}
				}
			}
		case eir.SynthPromoted, eir.SynthPtrWrapper, eir.SynthMethodExpr:
			if m != nil && declared(m) {
				return moves.Callee{Fn: m.Origin()}
			}
		case eir.SynthMethodValue:
			// The record holds the receiver: the call's inputs are the
			// method's parameters.
			if m != nil && declared(m) {
				return moves.Callee{Fn: m.Origin(), Shift: 1}
			}
		case eir.SynthVariant:
			return moves.Callee{} // makes a union value of its argument
		}
	}
	return moves.Callee{Unknown: true}
}

// declared reports whether m is a method with a declaration, not one an
// interface declares (whose calls reach further functions).
func declared(m *types2.Func) bool {
	sig, ok := m.Type().(*types2.Signature)
	return ok && (sig.Recv() == nil || !types2.IsInterface(sig.Recv().Type()))
}

// noteCall records the facts about the arguments of call c of f.
func noteCall(calls *moves.Calls, env solve.Env, ps domain.Paths, f *eir.Func, c *eir.Call) {
	noteCallees(calls, env, f, c)
	sl := slots(c)
	if len(sl) == 0 {
		return
	}
	// A dynamic call with no candidates never runs, and keeps nothing.
	cands := callees(env, f, c)
	passed := staticFuncs(env, f, c, cands)
	for i, s := range sl {
		if s == nil {
			continue
		}
		af := &moves.ArgFacts{}
		for _, cd := range cands {
			merge(af, argFacts(ps, cd, sl, i, passed))
		}
		for _, x := range s.exprs {
			calls.Add(x, af)
		}
	}
}

// merge adds what o says to af.
func merge(af, o *moves.ArgFacts) {
	af.Self = af.Self || o.Self
	af.Kept = af.Kept || o.Kept
	af.Returned = af.Returned || o.Returned
	af.Into = compat.Append(af.Into, o.Into...)
}

// staticFuncs returns, for a static call c of f, the summary of the
// declared function or capture-free literal each argument is as a func
// value (a static record, eir's OpClosure with no object), by the callee
// input it binds to; nil where it is not one, or its summary is not known.
// It returns nil unless a callee in cands defers a call.
func staticFuncs(env solve.Env, f *eir.Func, c *eir.Call, cands []callee) []*summary.Summary {
	if c.Kind != eir.CallStatic {
		return nil
	}
	deferred := false
	for _, cd := range cands {
		if cd.sum != nil && len(cd.sum.Deferred) > 0 {
			deferred = true
		}
	}
	if !deferred {
		return nil
	}
	defs := map[eir.Reg]*eir.Instr{}
	for _, b := range f.Blocks {
		for k := range b.Instrs {
			if x := &b.Instrs[k]; x.Op != eir.OpCall && x.Dst != 0 {
				defs[x.Dst] = x
			}
		}
	}
	prog := env.Program()
	out := compat.Make[*summary.Summary](len(c.Args))
	for in, r := range c.Args {
		if r == 0 {
			continue
		}
		for n := 0; n < 8; n++ {
			d := defs[r]
			if d == nil {
				break
			}
			if d.Op == eir.OpCopy {
				r = d.X
				continue
			}
			if d.Op == eir.OpClosure && d.Obj == eir.NoObj && d.Callee != eir.NoFunc && int(d.Callee) < len(prog.Funcs) && !prog.Funcs[d.Callee].Bodyless {
				out[in] = env.Summary(d.Callee)
			}
			break
		}
	}
	return out
}

// keepsNothing reports whether the summary sum keeps nothing of its input
// a past the call: it lets no key of a escape, stores none anywhere, and
// passes none to a call it defers.
func keepsNothing(sum *summary.Summary, a int) bool {
	for _, k := range sum.Esc {
		if k.Input() == a {
			return false
		}
	}
	for _, st := range sum.Store {
		if st.Src.Input() == a {
			return false
		}
	}
	for _, d := range sum.Deferred {
		if d.Fn.Input() == a {
			return false
		}
		for _, x := range d.Args {
			for _, k := range x.Keys {
				if k.Input() == a {
					return false
				}
			}
		}
	}
	return true
}

// argFacts returns what callee cd's summary says of its input i, given the
// call's slots. passed gives the summary of a static function the call
// passes for each callee input, for the calls the callee defers (D49).
func argFacts(ps domain.Paths, cd callee, sl []*slot, i int, passed []*summary.Summary) *moves.ArgFacts {
	af := pu.New(moves.ArgFacts{})
	if cd.sum == nil || i >= cd.inputs {
		// Nothing is known of the callee, or the input is one it does
		// not have (a candidate of another arity): the worst.
		af.Self, af.Kept, af.Returned = true, true, true
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
			af.Self, af.Kept = true, true
		}
	}
	for _, st := range sum.Store {
		if st.Src.Input() != i {
			continue
		}
		if self(st.Src) {
			af.Self, af.Kept = true, true
		}
		var dst ast.Expr
		direct := false
		if j := st.Dst.Input(); j < len(sl) && sl[j] != nil && !sl[j].packed_ && len(sl[j].exprs) == 1 {
			dst = sl[j].exprs[0]
			direct = sl[j].exact && ps.Loads(st.Dst.Path()) == 0 && !st.Dst.Deep() && !st.Dst.GE()
		}
		af.Into = compat.Append(af.Into, moves.Into{Arg: dst, Direct: direct, Loaded: !self(st.Src)})
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
	//
	// Where the call passes, for the function value, a declared function
	// or a literal that captures nothing, and that function keeps nothing
	// of the inputs it is given input i in, the deferred call keeps
	// nothing of i either.
	for _, d := range sum.Deferred {
		fn := d.Fn.Input() == i
		var target *summary.Summary
		if fi := d.Fn.Input(); !fn && ps.Loads(d.Fn.Path()) == 0 && !d.Fn.Deep() && !d.Fn.GE() && fi >= 0 && fi < len(passed) {
			target = passed[fi]
		}
		given := fn && self(d.Fn)
		af.Self = af.Self || given
		af.Kept = af.Kept || given
		for a, x := range d.Args {
			for _, k := range x.Keys {
				if k.Input() != i {
					continue
				}
				if target != nil && keepsNothing(target, a) {
					af.Returned = true
					continue
				}
				given = true
				af.Self = af.Self || self(k)
				af.Kept = af.Kept || self(k)
			}
		}
		if given {
			af.Into = compat.Append(af.Into, moves.Into{})
		}
		if fn || given {
			af.Returned = true
		}
	}
	return af
}
