package main

import (
	"fmt"
	"slices"
	"strings"
)

// Closures are function literals called where they are made, and the
// closures makers return: Plutonium builds those in regions their callers
// provide, and they keep their state, the variables they capture, from
// call to call. A returned closure must not see the maker's views of its
// own frame (slices of its arrays, pointers to its variables, closures it
// made), which Go would move to the heap and Plutonium rejects.

// closureSig picks the parameters and result of a closure: integers of
// the types intType gives, bools and strings.
func (g *gen) closureSig(ci *closureInfo, intType func() *typ) {
	switch {
	case g.chance(5):
		ci.result = tBool
	case g.chance(5):
		ci.result = tString
	default:
		ci.result = intType()
	}
	for i, n := 0, g.r.Intn(3); i < n; i++ {
		if g.chance(5) {
			ci.params = append(ci.params, tString)
		} else {
			ci.params = append(ci.params, intType())
		}
	}
}

// closureLit writes a function literal with ci's signature after head,
// and returns the cost of a call. A pure literal writes only its own
// variables; an impure one may also write whatever the enclosing function
// may. A returned literal outlives the enclosing function: it may write
// the variables it captures, its only state, but must not see the
// function's views of its own frame.
func (g *gen) closureLit(ci *closureInfo, head string, returned bool) int64 {
	outer := g.cx
	inner := &ctx{
		level:         outer.level + 1,
		results:       []*typ{ci.result},
		minWriteLevel: outer.level + 1,
		writeGlobal:   !ci.pure && outer.writeGlobal,
		mult:          1,
		budget:        fnBudget / 4,
		tparam:        outer.tparam,
		viewFloor:     outer.viewFloor,
	}
	if !ci.pure {
		inner.minWriteLevel = outer.minWriteLevel
	}
	if returned {
		inner.minWriteLevel = outer.level
		inner.viewFloor = inner.level
	}
	g.cx = inner
	g.push()
	var params []string
	for _, t := range ci.params {
		v := g.declare(&variable{name: g.name("a"), t: t, used: true})
		params = append(params, v.name+" "+t.String())
	}
	g.line("%sfunc(%s) %s {", head, strings.Join(params, ", "), ci.result)
	g.indent++
	g.stmts(1 + g.r.Intn(3))
	ret := g.returnValues()
	g.useAll()
	g.line("return %s", ret)
	g.sc = g.sc.parent
	g.indent--
	g.line("}")
	g.cx = outer
	return inner.cost + 1
}

// callClosure calls the closure variable name a few times.
func (g *gen) callClosure(name string, ci *closureInfo) {
	var v *variable
	for _, x := range g.visible() {
		if x.name == name {
			v = x
		}
	}
	for i, n := 0, g.r.Intn(3); i < n; i++ {
		if g.cx.cost+g.cx.mult*(ci.cost+1) > g.cx.budget || (!ci.pure && len(g.ranged) > 0) {
			break
		}
		g.addCost(ci.cost + 1)
		v.used = true
		r := &variable{name: g.name("v"), t: ci.result}
		g.line("%s := %s(%s)", r.name, name, g.args(ci.params, make([]int, len(ci.params))))
		g.declare(r)
	}
}

// funcType spells the type of a closure.
func funcType(ci *closureInfo) string {
	var ps []string
	for _, t := range ci.params {
		ps = append(ps, t.String())
	}
	return "func(" + strings.Join(ps, ", ") + ") " + ci.result.String()
}

// maker generates a function that returns a closure: a literal that
// captures its parameters and locals, chosen on a condition, or the
// closure returned by an earlier maker.
func (g *gen) maker(i int) *fnInfo {
	f := &fnInfo{name: fmt.Sprintf("mk%d", i), pure: g.chance(2)}
	ci := &closureInfo{}
	// The closure writes globals only if the maker may and it does.
	ci.pure = f.pure || g.chance(2)
	var same []*fnInfo
	for _, p := range g.funcs[:g.nfuncs] {
		if p.ret != nil && (p.pure || !f.pure) {
			same = append(same, p)
		}
	}
	switch {
	case len(same) > 0 && g.chance(2):
		p := same[g.r.Intn(len(same))]
		ci.params, ci.result = p.ret.params, p.ret.result
		ci.pure = ci.pure && p.ret.pure
	case g.ifc != nil && g.chance(3):
		// The signature of Ifc's Get, so that it may return a method value.
		ci.params, ci.result = []*typ{tInt}, tInt
	default:
		g.closureSig(ci, g.concreteInt)
	}
	f.ret = ci
	g.lines = nil
	g.indent = 0
	g.cx = &ctx{writeGlobal: !f.pure, mult: 1, budget: fnBudget, noReturn: true}
	g.push()
	var params []string
	for k, n := 0, g.r.Intn(4); k < n; k++ {
		t := g.randType(true)
		v := &variable{name: g.name("p"), t: t, used: true}
		if t.kind == kSlice {
			v.sliceLen = 1 + g.r.Intn(6)
		}
		g.declare(v)
		f.params = append(f.params, v)
		params = append(params, v.name+" "+t.String())
	}
	if g.chance(3) {
		g.line("//pu:noinline")
	}
	g.line("func %s(%s) %s {", f.name, strings.Join(params, ", "), funcType(ci))
	g.indent++
	h := g.declare(&variable{name: g.name("h"), t: tUint64, used: true})
	g.cx.hash = h
	g.line("var %s uint64", h.name)
	g.stmts(1 + g.r.Intn(3))
	if g.chance(2) {
		g.line("if %s {", g.boolExpr(2, true).s)
		g.indent++
		g.returnClosure(f, same)
		g.indent--
		g.line("}")
		g.stmts(g.r.Intn(3))
	}
	g.finalReturn(func() { g.returnClosure(f, same) }, func() {
		g.line("println(%q, %s)", f.name, h.name)
	})
	g.indent--
	g.line("}")
	g.line("")
	g.sc = g.sc.parent
	f.cost = g.cx.cost + 1
	f.text = strings.Join(g.lines, "\n") + "\n"
	return f
}

// finalReturn generates a function's last statement with ret, which may
// read any variable in scope, and puts before it the uses of the
// variables it left unread and then what before writes.
func (g *gen) finalReturn(ret, before func()) {
	mark := len(g.lines)
	ret()
	last := slices.Clone(g.lines[mark:])
	g.lines = g.lines[:mark]
	g.useAll()
	before()
	g.lines = append(g.lines, last...)
}

// returnClosure returns a new literal, or the closure of a call of an
// earlier maker of the same signature.
func (g *gen) returnClosure(f *fnInfo, earlier []*fnInfo) {
	ci := f.ret
	var cands []*fnInfo
	for _, p := range earlier {
		// Arguments may view this frame, which the closure outlives.
		if hasRefParam(p) {
			continue
		}
		if slices.Equal(p.ret.params, ci.params) && p.ret.result == ci.result && (p.ret.pure || !ci.pure) {
			cands = append(cands, p)
		}
	}
	if len(cands) > 0 && g.chance(3) {
		p := cands[g.r.Intn(len(cands))]
		g.addCost(p.cost + 1)
		ci.cost = max(ci.cost, p.ret.cost)
		g.line("return %s", g.callText(p, nil))
		return
	}
	if g.ifc != nil && len(ci.params) == 1 && ci.params[0] == tInt && ci.result == tInt && g.chance(2) {
		// A method value, which holds a copy of its receiver.
		ci.cost = max(ci.cost, fnBudget/4+1)
		g.line("return %s.Get", g.getReceiver())
		return
	}
	ci.cost = max(ci.cost, g.closureLit(ci, "return ", true))
}

// hasRefParam reports whether f has a slice or pointer parameter.
func hasRefParam(f *fnInfo) bool {
	for _, p := range f.params {
		if p.t.ref() {
			return true
		}
	}
	return false
}

// makerStmt calls a maker and then the closure it returns.
func (g *gen) makerStmt() bool {
	var cands []*fnInfo
	for _, f := range g.funcs[:g.nfuncs] {
		if f.ret != nil && (f.pure || g.cx.writeGlobal) && g.cx.cost+g.cx.mult*(f.cost+1) <= g.cx.budget {
			cands = append(cands, f)
		}
	}
	if len(cands) == 0 {
		return false
	}
	f := cands[g.r.Intn(len(cands))]
	g.addCost(f.cost + 1)
	name := g.name("c")
	g.line("%s := %s", name, g.callText(f, nil))
	// The closure may hold views of this frame passed to the maker.
	g.declare(&variable{name: name, closure: f.ret, owned: hasRefParam(f)})
	g.callClosure(name, f.ret)
	return true
}
