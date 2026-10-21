package sched

import (
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/types2"
)

// The instantiation closure (plan section (b), stage B): for every type
// parameter, the concrete types it is instantiated with anywhere in the
// program. An instantiation written inside a generic body names the body's
// own type parameters (F[T] calling G[[]T]); the closure substitutes every
// instantiation of those, until nothing grows.
//
// Sets are of concrete types only. A type parameter whose set cannot be
// bounded (a recursive chain F[T] calling F[[]T], or more combinations than
// the limits) is open: its set is not known, and every consumer must then
// assume any type (Facts.instOpen). Open is never a silent truncation.

// Limits of the closure. Past them a type parameter is open.
const (
	instMaxDepth  = 12   // nesting depth of a type argument
	instMaxSet    = 4096 // types per type parameter
	instMaxCombos = 4096 // substitutions one instantiation fact may need at once
)

// instEdge says that type parameter tp is instantiated with arg, which may
// mention type parameters of the generic code that wrote it.
type instEdge struct {
	tp  *types2.TypeParam
	arg types2.Type
}

type instClosure struct {
	tt     *typeTable
	alias  map[*types2.TypeParam]*types2.TypeParam // a method's receiver type parameter to its type's
	sets   map[*types2.TypeParam][]types2.Type     // concrete, interned
	ids    map[*types2.TypeParam]map[typeID]bool
	open   map[*types2.TypeParam]bool
	edges  []instEdge
	work   []int                       // edges to apply
	queued []bool                      // by edge: in work
	deps   map[*types2.TypeParam][]int // a type parameter to the edges whose arg mentions it
	seenN  map[*types2.Named]bool
}

func newInstClosure(tt *typeTable) *instClosure {
	return &instClosure{
		tt:    tt,
		alias: map[*types2.TypeParam]*types2.TypeParam{},
		sets:  map[*types2.TypeParam][]types2.Type{},
		ids:   map[*types2.TypeParam]map[typeID]bool{},
		open:  map[*types2.TypeParam]bool{},
		deps:  map[*types2.TypeParam][]int{},
		seenN: map[*types2.Named]bool{},
	}
}

// canon maps a method's receiver type parameter to the type parameter of
// its receiver's type, which instantiations of the type bind.
func (c *instClosure) canon(tp *types2.TypeParam) *types2.TypeParam {
	if a, ok := c.alias[tp]; ok {
		return a
	}
	return tp
}

// addMethodAliases records the receiver type parameters of a method of a
// generic type as aliases of the type's own.
func (c *instClosure) addMethodAliases(fn *types2.Func) {
	sig := fn.Signature()
	rtps := sig.RecvTypeParams()
	if rtps == nil || rtps.Len() == 0 || sig.Recv() == nil {
		return
	}
	rt := types2.Unalias(sig.Recv().Type())
	if p, ok := rt.(*types2.Pointer); ok {
		rt = types2.Unalias(p.Elem())
	}
	n, ok := rt.(*types2.Named)
	if !ok {
		return
	}
	tps := n.Origin().TypeParams()
	for i := 0; i < rtps.Len() && tps != nil && i < tps.Len(); i++ {
		if rtps.At(i) != tps.At(i) {
			c.alias[rtps.At(i)] = tps.At(i)
		}
	}
}

// typeParamsOf returns the type parameters an instantiation of obj binds.
func typeParamsOf(obj types2.Object) *types2.TypeParamList {
	switch obj := obj.(type) {
	case *types2.Func:
		sig := obj.Signature()
		if tps := sig.TypeParams(); tps != nil && tps.Len() > 0 {
			return tps
		}
		return sig.RecvTypeParams()
	case *types2.TypeName:
		if n, ok := types2.Unalias(obj.Type()).(*types2.Named); ok {
			return n.Origin().TypeParams()
		}
	}
	return nil
}

// addInstance records an instantiation of obj with args.
func (c *instClosure) addInstance(obj types2.Object, args []types2.Type) {
	tps := typeParamsOf(obj)
	if tps == nil {
		return
	}
	for i := 0; i < tps.Len() && i < len(args); i++ {
		c.addEdge(tps.At(i), args[i])
	}
	for _, a := range args {
		c.noteType(a)
	}
}

func (c *instClosure) addEdge(tp *types2.TypeParam, arg types2.Type) {
	if arg == nil {
		return
	}
	idx := len(c.edges)
	c.edges = append(c.edges, instEdge{tp: c.canon(tp), arg: arg})
	c.queued = append(c.queued, false)
	c.enqueue(idx)
	for _, q := range typeParamsIn(arg) {
		q = c.canon(q)
		c.deps[q] = append(c.deps[q], idx)
	}
}

// noteType records the instantiations a type mentions: every instantiated
// named type in it binds its origin's type parameters.
func (c *instClosure) noteType(t types2.Type) {
	walkType(t, func(u types2.Type) bool {
		n, ok := u.(*types2.Named)
		if !ok || c.seenN[n] {
			return true
		}
		c.seenN[n] = true
		args := n.TypeArgs()
		tps := n.Origin().TypeParams()
		if args == nil || tps == nil {
			return true
		}
		for i := 0; i < args.Len() && i < tps.Len(); i++ {
			c.addEdge(tps.At(i), args.At(i))
		}
		return true
	})
}

// solve runs the closure to its fixed point. Edges added while it runs
// (instantiated types found by substitution mention further named types)
// are queued as they are added.
func (c *instClosure) solve() {
	for len(c.work) > 0 {
		i := c.work[len(c.work)-1]
		c.work = c.work[:len(c.work)-1]
		c.queued[i] = false
		for _, tp := range c.apply(c.edges[i]) {
			for _, j := range c.deps[tp] {
				c.enqueue(j)
			}
		}
	}
}

func (c *instClosure) enqueue(i int) {
	if !c.queued[i] {
		c.queued[i] = true
		c.work = append(c.work, i)
	}
}

// apply adds what edge e gives its type parameter now, and returns the type
// parameters that changed (grew, or became open).
func (c *instClosure) apply(e instEdge) []*types2.TypeParam {
	if c.open[e.tp] {
		return nil
	}
	qs := typeParamsIn(e.arg)
	if len(qs) == 0 {
		if c.add(e.tp, e.arg) {
			return []*types2.TypeParam{e.tp}
		}
		return nil
	}
	combos := 1
	choices := make([][]types2.Type, len(qs))
	for i, q := range qs {
		cq := c.canon(q)
		if c.open[cq] {
			return c.setOpen(e.tp)
		}
		choices[i] = c.sets[cq]
		combos *= len(choices[i])
		if combos == 0 {
			return nil // nothing to substitute yet
		}
		if combos > instMaxCombos {
			return c.setOpen(e.tp)
		}
	}
	changed := false
	pick := make([]types2.Type, len(qs))
	var rec func(i int) bool
	rec = func(i int) bool {
		if i == len(qs) {
			t := types2.Subst(nil, e.arg, qs, pick)
			if typeDepth(t, instMaxDepth) > instMaxDepth {
				return false
			}
			if c.add(e.tp, t) {
				changed = true
			}
			return !c.open[e.tp]
		}
		for _, t := range choices[i] {
			pick[i] = t
			if !rec(i + 1) {
				return false
			}
		}
		return true
	}
	if !rec(0) {
		return c.setOpen(e.tp)
	}
	if changed {
		return []*types2.TypeParam{e.tp}
	}
	return nil
}

// add adds a concrete type to tp's set and reports whether it is new.
func (c *instClosure) add(tp *types2.TypeParam, t types2.Type) bool {
	if c.tt.hasTypeParam(t) {
		// A type parameter that no substitution reached (one the closure
		// does not know to bind): the set cannot be bounded.
		c.setOpen(tp)
		return true
	}
	id := c.tt.intern(t)
	m := c.ids[tp]
	if m == nil {
		m = map[typeID]bool{}
		c.ids[tp] = m
	}
	if m[id] {
		return false
	}
	m[id] = true
	c.sets[tp] = append(c.sets[tp], c.tt.types[id])
	c.noteType(t)
	if len(c.sets[tp]) > instMaxSet {
		c.setOpen(tp)
	}
	return true
}

func (c *instClosure) setOpen(tp *types2.TypeParam) []*types2.TypeParam {
	if c.open[tp] {
		return nil
	}
	c.open[tp] = true
	c.sets[tp] = nil
	return []*types2.TypeParam{tp}
}

// collectInstances feeds the closure from every function's facts and from
// the instantiated named types the program's types mention.
func (c *instClosure) collect(p *eir.Program) {
	for _, f := range p.Funcs {
		if f.Key.Obj != nil {
			c.addMethodAliases(f.Key.Obj)
		}
	}
	for _, f := range p.Funcs {
		for _, in := range f.Facts.Instances {
			c.addInstance(in.Obj, in.Args)
		}
		for _, t := range f.Facts.Converted {
			c.noteType(t)
		}
		if f.Sig != nil {
			c.noteType(f.Sig)
		}
		for _, in := range f.Inputs {
			c.noteType(in.Type)
		}
		for _, o := range f.Objects {
			c.noteType(o.Type)
		}
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				c.noteType(b.Instrs[i].Type)
				c.noteType(b.Instrs[i].To)
			}
		}
	}
}

// run collects the facts and solves the closure.
func (c *instClosure) run(p *eir.Program) {
	c.collect(p)
	c.solve()
}

// instantiated returns tp's set and whether it is known (not open).
func (c *instClosure) instantiated(tp *types2.TypeParam) ([]types2.Type, bool) {
	tp = c.canon(tp)
	if c.open[tp] {
		return nil, false
	}
	return c.sets[tp], true
}
