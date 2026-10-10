package main

import (
	"fmt"
	"strings"
)

// Iterators are makers whose closure is a range function, ranged over
// with for range: Go 1.23's range over func, which both compilers lower
// by turning the loop body into a yield function. The iterator yields an
// index in [0, trips) and a value, only the value, or nothing, and stops
// when yield returns false, as it must.

type iterInfo struct {
	val   *typ // the value yielded after the index, or nil
	index bool // whether an index is yielded
	trips int64
}

// yieldType spells the type of an iterator's yield function.
func yieldType(it *iterInfo) string {
	var ps []string
	if it.index {
		ps = append(ps, "int")
	}
	if it.val != nil {
		ps = append(ps, it.val.String())
	}
	return "func(" + strings.Join(ps, ", ") + ") bool"
}

// iterMaker generates a function that returns an iterator: a range
// function that captures the maker's parameters and locals.
func (g *gen) iterMaker(i int) *fnInfo {
	f := &fnInfo{name: fmt.Sprintf("seq%d", i), pure: g.chance(2)}
	g.cx = &ctx{writeGlobal: !f.pure, mult: 1, budget: fnBudget, noReturn: true}
	it := &iterInfo{trips: int64(1 + g.r.Intn(8))}
	switch g.r.Intn(4) {
	case 0:
		// Neither index nor value.
	case 1:
		it.val = g.randType(false)
	default:
		it.index = true
		it.val = g.randType(false)
	}
	f.iter = it
	g.lines = nil
	g.indent = 0
	g.push()
	var params []string
	for k, n := 0, g.r.Intn(3); k < n; k++ {
		// No slices or pointers: the iterator is ranged over where it is
		// made, but arguments may view the caller's frame, and keeping
		// the cases apart is not worth it.
		t := g.randType(false)
		v := g.declare(&variable{name: g.name("p"), t: t, used: true})
		f.params = append(f.params, v)
		params = append(params, v.name+" "+t.String())
	}
	g.line("func %s(%s) func(yield %s) {", f.name, strings.Join(params, ", "), yieldType(it))
	g.indent++
	h := g.declare(&variable{name: g.name("h"), t: tUint64, used: true})
	g.cx.hash = h
	g.line("var %s uint64", h.name)
	g.stmts(g.r.Intn(3))
	var innerCost int64
	g.finalReturn(func() { innerCost = g.rangeFuncLit(f, it) }, func() {
		g.line("println(%q, %s)", f.name, h.name)
	})
	g.indent--
	g.line("}")
	g.line("")
	g.sc = g.sc.parent
	f.cost = g.cx.cost + innerCost + 1
	f.text = strings.Join(g.lines, "\n") + "\n"
	return f
}

// rangeFunc ranges over an iterator, with the loop body as its yield
// function.
func (g *gen) rangeFunc() bool {
	var cands []*fnInfo
	for _, f := range g.funcs[:g.nfuncs] {
		if f.iter != nil && (f.pure || g.cx.writeGlobal) && f.iter.trips <= g.tripLimit()*2 {
			cands = append(cands, f)
		}
	}
	if len(cands) == 0 {
		return false
	}
	f := cands[g.r.Intn(len(cands))]
	it := f.iter
	g.addCost(f.cost + 1)
	var vars []*variable
	var names []string
	if it.index {
		k := &variable{name: g.name("k"), t: tInt, readonly: true, hasRange: true, lo: 0, hi: it.trips - 1}
		vars = append(vars, k)
		names = append(names, k.name)
	}
	if it.val != nil {
		v := &variable{name: g.name("e"), t: it.val, readonly: true}
		vars = append(vars, v)
		names = append(names, v.name)
	}
	call := g.callText(f, nil)
	switch {
	case len(names) == 0:
		g.line("for range %s {", call)
	case it.index && g.chance(4):
		// Only the index.
		vars = vars[:1]
		g.line("for %s := range %s {", names[0], call)
	default:
		g.line("for %s := range %s {", strings.Join(names, ", "), call)
	}
	g.loopBody(it.trips, vars...)
	return true
}

// rangeFuncLit returns an iterator's range function, a literal with its
// own state, and returns the cost of a call.
func (g *gen) rangeFuncLit(f *fnInfo, it *iterInfo) int64 {
	outer := g.cx
	inner := &ctx{
		level:         1,
		minWriteLevel: 0,
		writeGlobal:   outer.writeGlobal,
		mult:          1,
		budget:        fnBudget / 4,
		viewFloor:     1,
		noReturn:      true,
	}
	g.cx = inner
	g.push()
	y := g.name("yield")
	g.line("return func(%s %s) {", y, yieldType(it))
	g.indent++
	g.stmts(g.r.Intn(2))
	i0 := &variable{name: g.name("i"), t: tInt, readonly: true, used: true, hasRange: true, lo: 0, hi: it.trips - 1}
	g.line("for %s := 0; %s < %d; %s++ {", i0.name, i0.name, it.trips, i0.name)
	g.indent++
	g.push()
	g.declare(i0)
	g.cx.mult = it.trips
	g.stmts(g.r.Intn(2))
	var args []string
	if it.index {
		args = append(args, i0.name)
	}
	if it.val != nil {
		args = append(args, g.value(it.val, 2))
	}
	g.line("if !%s(%s) {", y, strings.Join(args, ", "))
	g.line("\tprintln(%q, %s)", f.name+" stopped", i0.name)
	g.line("\treturn")
	g.line("}")
	g.pop()
	g.cx.mult = 1
	g.indent--
	g.line("}")
	g.stmts(g.r.Intn(2))
	g.useAll()
	g.sc = g.sc.parent
	g.indent--
	g.line("}")
	g.cx = outer
	return inner.cost + 1
}

// rangeAndPrint ranges over an iterator and prints what it yields.
func (g *gen) rangeAndPrint(f *fnInfo) {
	it := f.iter
	g.addCost(f.cost + 1)
	call := g.callText(f, nil)
	var names []string
	if it.index {
		names = append(names, g.name("k"))
	}
	if it.val != nil {
		names = append(names, g.name("e"))
	}
	if len(names) == 0 {
		g.line("for range %s {", call)
		g.line("\tprintln(%q)", f.name+" yields")
		g.line("}")
		return
	}
	g.line("for %s := range %s {", strings.Join(names, ", "), call)
	g.indent++
	if it.index {
		g.line("println(%q, %s)", f.name+" yields", names[0])
	}
	if it.val != nil {
		g.printLeaves(names[len(names)-1], it.val)
	}
	g.indent--
	g.line("}")
}
