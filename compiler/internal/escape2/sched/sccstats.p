package sched

import (
	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/os"
	"pluto/slices"
)

// sccStatsOn (PLUTO_ESCAPE2_SCC_STATS=1) prints, on standard error after
// the analysis, where the escape analysis spends its steps: the SCCs that
// took the most, where the largest one's members come from, the kinds of
// its edges, the size of the largest SCC with some kinds of edges left
// out, the deferred calls whose fallback joins add the most edges to it,
// the SCC of the reads the solves made, and, for the functions with the
// most steps, what made each of their solves run again and whether the
// summary changed. A measurement: it changes nothing the analysis does.
var sccStatsOn = compat.Getenv("PLUTO_ESCAPE2_SCC_STATS") != ""

// The kinds of an edge f -> g (f reads g's summary), as bits.
const (
	kStatic      = 1
	kIface       = 2
	kTypeParam   = 4
	kFunc        = 8
	kDefClosure  = 16 // a closure literal a deferred call reaches
	kDefFallback = 32 // the fallback join of a deferred call f cannot defer
	kNone        = 64 // a trigger with no edge of the graph
	nKinds       = 7
)

var kindNames = []string{"static", "iface", "typeparam", "func", "def-closure", "def-fallback", "none"}

type statEdge struct {
	to   eir.FuncID
	mask int
}

// statRec is one solve: its steps, the kinds and number of the summaries
// that grew since the function's previous solve and that it had read
// (none for its first solve), whether its own summary grew, and whether
// it was the final solve.
type statRec struct {
	steps   int64
	trig    int
	ntrig   int
	changed bool
	final   bool
}

var (
	statSteps   []int64
	statNanos   []int64
	statSolves  []int64
	statLast    []int64
	statComp    []int32
	statLargest int32
	statEdges   [][]statEdge
	statKind    map[[2]eir.FuncID]int
	statTrig    [][]eir.FuncID
	statCur     []statRec
	statRecs    [][]statRec
	statReads   map[[2]eir.FuncID]bool
	statSplitNG int64
	// Deferred calls by the fallback edges they add inside the largest
	// SCC, the size of their fallback set, and the functions resolving
	// them there.
	statOrigEdges map[string]int64
	statOrigSize  map[string]int64
	statOrigUses  map[string]int64
)

func sccStatsBegin(p *eir.Program, f *facts, comps [][]eir.FuncID) {
	if !sccStatsOn {
		return
	}
	n := len(p.Funcs)
	statSteps = compat.Make[int64](n)
	statNanos = compat.Make[int64](n)
	statSolves = compat.Make[int64](n)
	statLast = compat.Make[int64](n)
	statComp = compat.Make[int32](n)
	for i := range statComp {
		statComp[i] = -1
	}
	statLargest = 0
	for ci, comp := range comps {
		for _, id := range comp {
			statComp[id] = int32(ci)
		}
		if len(comp) > len(comps[statLargest]) {
			statLargest = int32(ci)
		}
	}
	statEdges = compat.Make[[]statEdge](n)
	statKind = map[[2]eir.FuncID]int{}
	statTrig = compat.Make[[]eir.FuncID](n)
	statCur = compat.Make[statRec](n)
	statRecs = compat.Make[[]statRec](n)
	statReads = map[[2]eir.FuncID]bool{}
	statOrigEdges = map[string]int64{}
	statOrigSize = map[string]int64{}
	statOrigUses = map[string]int64{}
	for _, fn := range p.Funcs {
		statBuildEdges(p, f, fn, comps)
	}
	statPrev = solve.Measure
	solve.Measure = statMeasure
}

func statKindOf(k eir.CallKind) int {
	switch k {
	case eir.CallStatic:
		return kStatic
	case eir.CallIface:
		return kIface
	case eir.CallTypeParam:
		return kTypeParam
	case eir.CallFunc:
		return kFunc
	}
	return 0
}

// statBuildEdges records fn's edges with their kinds, as buildGraphD
// makes them, with the deferral edges split into closure and fallback.
func statBuildEdges(p *eir.Program, f *facts, fn *eir.Func, comps [][]eir.FuncID) {
	n := len(p.Funcs)
	masks := map[eir.FuncID]int{}
	for _, b := range fn.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != eir.OpCall || in.Call == nil {
				continue
			}
			c := in.Call
			k := statKindOf(c.Kind)
			if k == 0 {
				continue
			}
			if c.Kind == eir.CallStatic {
				if c.Callee != eir.NoFunc && int(c.Callee) < n {
					masks[c.Callee] |= k
				}
				continue
			}
			for _, id := range f.rule.callees(fn, c) {
				masks[id] |= k
			}
		}
	}
	if f.def != nil {
		closure := map[eir.FuncID]bool{}
		fallback := map[eir.FuncID]bool{}
		inLargest := statComp[fn.ID] == statLargest
		statSplit(f.def, fn, closure, func(o origin) {
			ids := f.def.fallbackOf(o)
			key := ""
			if inLargest {
				key = statOriginName(p, o)
				statOrigSize[key] = int64(len(ids))
				statOrigUses[key]++
			}
			for _, id := range ids {
				fallback[id] = true
				if inLargest && int(id) < n && statComp[id] == statLargest {
					statOrigEdges[key]++
				}
			}
		})
		for id := range closure {
			masks[id] |= kDefClosure
		}
		for id := range fallback {
			masks[id] |= kDefFallback
		}
		// The split must give what the graph used.
		deps := f.def.deps(fn)
		both := len(closure)
		for id := range fallback {
			if !closure[id] {
				both++
			}
		}
		if both != len(deps) {
			statSplitNG++
		} else {
			for _, id := range deps {
				if !closure[id] && !fallback[id] {
					statSplitNG++
					break
				}
			}
		}
	}
	var es []statEdge
	for id, m := range masks {
		if id == eir.NoFunc || int(id) >= n {
			continue
		}
		es = compat.Append(es, statEdge{id, m})
		if c := statComp[fn.ID]; c >= 0 && c == statComp[id] && len(comps[c]) > 1 {
			statKind[[2]eir.FuncID{fn.ID, id}] = m
		}
	}
	slices.SortFunc(es, func(a, b statEdge) int { return int(a.to) - int(b.to) })
	statEdges[fn.ID] = es
}

func statOriginName(p *eir.Program, o origin) string {
	c := o.call
	desc := "?"
	switch c.Kind {
	case eir.CallStatic:
		if c.Callee != eir.NoFunc && int(c.Callee) < len(p.Funcs) {
			desc = p.Funcs[c.Callee].Name
		}
	case eir.CallIface, eir.CallTypeParam:
		if c.Method != nil {
			desc = c.Method.FullName()
			if c.Iface != nil {
				desc = types2.TypeString(c.Iface, nil) + "." + c.Method.Name()
			}
		}
	case eir.CallFunc:
		if c.Sig != nil {
			desc = types2.TypeString(c.Sig, nil)
		}
	}
	return p.Funcs[o.f].Name + " -> " + callKindName(c.Kind) + " " + desc
}

type statItem struct {
	os []origin
	r  eir.Reg
}

// statSplit is resolve with its deps split: the closure literals it reads
// into closure, and each deferred call whose fallback it joins passed to
// onFallback (once per time resolve joins it).
func statSplit(d *deferral, f *eir.Func, closure map[eir.FuncID]bool, onFallback func(o origin)) {
	if !d.final {
		return
	}
	done := map[eir.Reg]map[*eir.Call]bool{}
	var work []statItem
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != eir.OpCall {
				continue
			}
			d.eachPassed(f, in.Call, func(_ eir.FuncID, _ int, a eir.Reg, os []origin) {
				work = compat.Append(work, statItem{os, a})
			})
		}
	}
	defs := d.c.defsOf(f)
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			in := &b.Instrs[i]
			if in.Op != eir.OpCall {
				continue
			}
			d.eachArgPassed(f, in.Call, func(g eir.FuncID, a eir.Reg) {
				if mayFramePtr(d, f, defs, a, map[eir.Reg]bool{}) {
					for _, os := range d.inputs[g] {
						for _, o := range os {
							onFallback(o)
						}
					}
				}
			})
		}
	}
	for len(work) > 0 {
		it := work[len(work)-1]
		work = work[:len(work)-1]
		var os []origin
		for _, o := range it.os {
			if done[it.r] == nil {
				done[it.r] = map[*eir.Call]bool{}
			}
			if !done[it.r][o.call] {
				done[it.r][o.call] = true
				os = compat.Append(os, o)
			}
		}
		if len(os) == 0 {
			continue
		}
		rch := d.reachOf(f, it.r)
		if rch.unknown {
			for _, o := range os {
				onFallback(o)
			}
		}
		for _, cr := range rch.closures {
			l := defs[cr].Callee
			closure[l] = true
			for j, os2 := range d.inputs[l] {
				if d.isRecord(l, j) {
					work = compat.Append(work, statItem{os2, cr})
				} else {
					for _, o := range os2 {
						onFallback(o)
					}
				}
			}
		}
	}
}

var statPrev func(f *eir.Func, m solve.Measurement)

func statMeasure(f *eir.Func, m solve.Measurement) {
	if int(f.ID) < len(statSteps) {
		statSteps[f.ID] += int64(m.Steps)
		statLast[f.ID] = int64(m.Steps)
	}
	if statPrev != nil {
		statPrev(f, m)
	}
}

// sccStatsTake is called with the reads of a solve that is not final,
// before the round's summaries grow: what grew since the function's
// previous solve is what made this one run.
func sccStatsTake(id eir.FuncID, reads []eir.FuncID) {
	if statRecs == nil {
		return
	}
	r := statRec{}
	seen := map[eir.FuncID]bool{}
	for _, g := range statTrig[id] {
		if seen[g] {
			continue
		}
		seen[g] = true
		r.ntrig++
		if m, ok := statKind[[2]eir.FuncID{id, g}]; ok {
			r.trig |= m
		} else {
			r.trig |= kNone
		}
	}
	statTrig[id] = nil
	statCur[id] = r
	for _, rd := range reads {
		statReads[[2]eir.FuncID{id, rd}] = true
	}
}

func sccStatsSolve(id eir.FuncID, nanos int64, final bool) {
	if statRecs == nil {
		return
	}
	statNanos[id] += nanos
	statSolves[id]++
	r := statRec{final: true}
	if !final {
		r = statCur[id]
		statCur[id] = statRec{}
	}
	r.steps = statLast[id]
	statRecs[id] = compat.Append(statRecs[id], r)
}

// sccStatsGrew is called when id's summary grew, with the members that
// have read it.
func sccStatsGrew(id eir.FuncID, readers []eir.FuncID) {
	if statRecs == nil {
		return
	}
	if rs := statRecs[id]; len(rs) > 0 {
		rs[len(rs)-1].changed = true
	}
	for _, rd := range readers {
		statTrig[rd] = compat.AppendPlain(statTrig[rd], id)
	}
}

func statPkg(f *eir.Func) string {
	if f.Pkg == nil {
		return "?"
	}
	return f.Pkg.Path()
}

func statLine(format string, args ...compat.Arg) {
	compat.Fprintfb(os.Stderr, "escape2: sccstats: "+format+"\n", args...)
}

type statCount struct {
	key string
	n   int64
}

func statTop(m map[string]int64, k int) []statCount {
	var out []statCount
	for key, n := range m {
		out = compat.Append(out, statCount{key, n})
	}
	slices.SortFunc(out, func(a, b statCount) int {
		if a.n != b.n {
			if a.n > b.n {
				return -1
			}
			return 1
		}
		if a.key < b.key {
			return -1
		}
		if a.key > b.key {
			return 1
		}
		return 0
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}

func statMaskName(m int) string {
	if m == 0 {
		return "first"
	}
	s := ""
	for k := 0; k < nKinds; k++ {
		if m&(1<<k) != 0 {
			if s != "" {
				s += "+"
			}
			s += kindNames[k]
		}
	}
	return s
}

// largestWith returns the members of the largest SCC of the graph with
// only the edges of the kinds in keep.
func largestWith(n int, keep int) []eir.FuncID {
	g := pu_newGraph(n)
	for id, es := range statEdges {
		for _, e := range es {
			if e.mask&keep != 0 {
				g.succ[id] = compat.AppendPlain(g.succ[id], int32(e.to))
			}
		}
	}
	return largestOf(g)
}

func largestOf(g *callGraph) []eir.FuncID {
	var best []eir.FuncID
	for _, comp := range g.sccs() {
		if len(comp) > len(best) {
			best = comp
		}
	}
	return best
}

func pu_newGraph(n int) *callGraph {
	return pu.New(callGraph{nFuncs: n, succ: compat.Make[[]int32](n), deps: compat.Make[[]eir.FuncID](n)})
}

func callKindName(k eir.CallKind) string {
	switch k {
	case eir.CallStatic:
		return "static"
	case eir.CallIface:
		return "iface"
	case eir.CallTypeParam:
		return "typeparam"
	case eir.CallFunc:
		return "func"
	}
	return ""
}

type sccSum struct {
	ci                   int
	steps, nanos, solves int64
}

type statVariant struct {
	name string
	keep int
}

func nz(x int64) int64 {
	if x == 0 {
		return 1
	}
	return x
}

// statClass sums solves of one class.
type statClass struct {
	solves, changed, steps, unchangedSteps int64
}

func (c *statClass) add(r statRec) {
	c.solves++
	c.steps += r.steps
	if r.changed {
		c.changed++
	} else {
		c.unchangedSteps += r.steps
	}
}

func statClassLine(name string, c statClass) {
	statLine("    %s: %d solves, %d changed, %d steps, %d steps in unchanged", compat.ArgS(name), compat.ArgI(c.solves), compat.ArgI(c.changed), compat.ArgI(c.steps), compat.ArgI(c.unchangedSteps))
}

func sccStatsReport(p *eir.Program, f *facts, comps [][]eir.FuncID, inSCC []int32) {
	if !sccStatsOn {
		return
	}
	n := len(p.Funcs)
	var totSteps, totNanos, totSolves int64
	for id := range statSteps {
		totSteps += statSteps[id]
		totNanos += statNanos[id]
		totSolves += statSolves[id]
	}
	statLine("total %d SCCs, %d solves, %d steps, %d ms of solves", compat.ArgI(int64(len(comps))), compat.ArgI(totSolves), compat.ArgI(totSteps), compat.ArgI(totNanos/1000000))
	statLine("deferral split mismatches: %d", compat.ArgI(statSplitNG))
	sums := compat.Make[sccSum](len(comps))
	for ci, comp := range comps {
		sums[ci].ci = ci
		for _, id := range comp {
			sums[ci].steps += statSteps[id]
			sums[ci].nanos += statNanos[id]
			sums[ci].solves += statSolves[id]
		}
	}
	bySteps := compat.Append([]sccSum(nil), sums...)
	slices.SortFunc(bySteps, func(a, b sccSum) int {
		if a.steps > b.steps {
			return -1
		}
		if a.steps < b.steps {
			return 1
		}
		return a.ci - b.ci
	})
	for i := 0; i < len(bySteps) && i < 12; i++ {
		s := bySteps[i]
		comp := comps[s.ci]
		// The member with the most steps names the SCC.
		top := comp[0]
		for _, id := range comp {
			if statSteps[id] > statSteps[top] {
				top = id
			}
		}
		statLine("scc %d: %d funcs, %d solves, %d steps (%d per mille), %d ms (%d per mille), top %s", compat.ArgI(int64(s.ci)), compat.ArgI(int64(len(comp))), compat.ArgI(s.solves), compat.ArgI(s.steps), compat.ArgI(s.steps*1000/nz(totSteps)), compat.ArgI(s.nanos/1000000), compat.ArgI(s.nanos*1000/nz(totNanos)), compat.ArgS(p.Funcs[top].Name))
	}
	var sizes []int
	for _, comp := range comps {
		if len(comp) >= 20 {
			sizes = compat.AppendPlain(sizes, len(comp))
		}
	}
	slices.Sort(sizes)
	slices.Reverse(sizes)
	var sz []byte
	for _, k := range sizes {
		sz = compat.AppendInt(sz, int64(k), 10)
		sz = compat.AppendPlain(sz, ' ')
	}
	statLine("SCCs of 20 functions or more: %s", compat.ArgS(compat.String(sz)))

	largest := int(statLargest)
	comp := comps[largest]
	statLine("largest scc %d: %d funcs", compat.ArgI(int64(largest)), compat.ArgI(int64(len(comp))))
	pkgs := map[string]int64{}
	pkgSteps := map[string]int64{}
	for _, id := range comp {
		pkgs[statPkg(p.Funcs[id])]++
		pkgSteps[statPkg(p.Funcs[id])] += statSteps[id]
	}
	for _, c := range statTop(pkgs, 40) {
		statLine("  pkg %s: %d funcs, %d steps", compat.ArgS(c.key), compat.ArgI(c.n), compat.ArgI(pkgSteps[c.key]))
	}

	// Edges inside the largest SCC by kind, and by their exact set of
	// kinds; then the reads the solves made there.
	in := func(id eir.FuncID) bool { return int(id) < len(inSCC) && int(inSCC[id]) == largest }
	byKind := map[string]int64{}
	byMask := map[string]int64{}
	var nEdges int64
	for _, id := range comp {
		for _, e := range statEdges[id] {
			if !in(e.to) {
				continue
			}
			nEdges++
			byMask[statMaskName(e.mask)]++
			for k := 0; k < nKinds; k++ {
				if e.mask&(1<<k) != 0 {
					byKind[kindNames[k]]++
				}
			}
		}
	}
	statLine("  edges inside: %d", compat.ArgI(nEdges))
	for _, c := range statTop(byKind, 10) {
		statLine("  edges with %s: %d", compat.ArgS(c.key), compat.ArgI(c.n))
	}
	for _, c := range statTop(byMask, 16) {
		statLine("  edges exactly %s: %d", compat.ArgS(c.key), compat.ArgI(c.n))
	}

	// (a) The largest SCC with kinds of edges left out.
	all := kStatic | kIface | kTypeParam | kFunc | kDefClosure | kDefFallback
	variants := []statVariant{
		{"all edges", all},
		{"no deferral", all - kDefClosure - kDefFallback},
		{"no deferral fallback", all - kDefFallback},
		{"no deferral closure", all - kDefClosure},
		{"no iface", all - kIface},
		{"no iface, deferral fallback", all - kIface - kDefFallback},
		{"static and deferral closure", kStatic | kDefClosure},
		{"static and deferral fallback", kStatic | kDefFallback},
		{"static only", kStatic},
	}
	for _, v := range variants {
		best := largestWith(n, v.keep)
		pk := map[string]int64{}
		for _, id := range best {
			pk[statPkg(p.Funcs[id])]++
		}
		desc := ""
		for _, c := range statTop(pk, 6) {
			desc += compat.Sprintfb(" %s:%d", compat.ArgS(c.key), compat.ArgI(c.n))
		}
		statLine("(a) largest with %s: %d funcs;%s", compat.ArgS(v.name), compat.ArgI(int64(len(best))), compat.ArgS(desc))
	}

	// (b) The deferred calls whose fallback joins add the most edges
	// inside the largest SCC.
	for _, c := range statTop(statOrigEdges, 30) {
		statLine("(b) origin %s: %d edges inside, fallback of %d, joined %d times", compat.ArgS(c.key), compat.ArgI(c.n), compat.ArgI(statOrigSize[c.key]), compat.ArgI(statOrigUses[c.key]))
	}
	statLine("(b) %d deferred calls joined inside the largest SCC", compat.ArgI(int64(len(statOrigUses))))

	// (c) The SCC of the reads the solves made.
	rg := pu_newGraph(n)
	readMask := map[string]int64{}
	var nReads int64
	for e := range statReads {
		rg.succ[e[0]] = compat.AppendPlain(rg.succ[e[0]], int32(e[1]))
		if in(e[0]) && in(e[1]) {
			nReads++
			m, ok := statKind[e]
			if !ok {
				m = kNone
			}
			readMask[statMaskName(m)]++
		}
	}
	rbest := largestOf(rg)
	rIn := 0
	for _, id := range rbest {
		if in(id) {
			rIn++
		}
	}
	statLine("(c) read edges inside the largest SCC: %d of %d graph edges", compat.ArgI(nReads), compat.ArgI(nEdges))
	for _, c := range statTop(readMask, 16) {
		statLine("(c) read edges exactly %s: %d", compat.ArgS(c.key), compat.ArgI(c.n))
	}
	statLine("(c) largest SCC of the reads: %d funcs (%d of them in the largest SCC)", compat.ArgI(int64(len(rbest))), compat.ArgI(int64(rIn)))

	// (d) What made the solves in the largest SCC run again.
	var first, finals, resolve statClass
	only := map[int]*statClass{}
	incl := compat.Make[statClass](nKinds)
	for _, id := range comp {
		for _, r := range statRecs[id] {
			switch {
			case r.final:
				finals.add(r)
			case r.trig == 0:
				first.add(r)
			default:
				resolve.add(r)
				c := only[r.trig]
				if c == nil {
					c = pu.New(statClass{})
					only[r.trig] = c
				}
				c.add(r)
				for k := 0; k < nKinds; k++ {
					if r.trig&(1<<k) != 0 {
						incl[k].add(r)
					}
				}
			}
		}
	}
	statLine("(d) largest SCC solves:")
	statClassLine("first", first)
	statClassLine("re-solves", resolve)
	statClassLine("final", finals)
	for k := 0; k < nKinds; k++ {
		statClassLine("re-solves including "+kindNames[k], incl[k])
	}
	var masks []int
	for m := range only {
		masks = compat.AppendPlain(masks, m)
	}
	slices.SortFunc(masks, func(a, b int) int {
		if only[a].steps > only[b].steps {
			return -1
		}
		if only[a].steps < only[b].steps {
			return 1
		}
		return a - b
	})
	for i, m := range masks {
		if i == 16 {
			break
		}
		statClassLine("re-solves triggered exactly by "+statMaskName(m), *only[m])
	}

	// The ten functions with the most steps.
	ids := compat.Append([]eir.FuncID(nil), comp...)
	slices.SortFunc(ids, func(a, b eir.FuncID) int {
		if statSteps[a] > statSteps[b] {
			return -1
		}
		if statSteps[a] < statSteps[b] {
			return 1
		}
		return int(a) - int(b)
	})
	for i := 0; i < len(ids) && i < 10; i++ {
		id := ids[i]
		rs := statRecs[id]
		var st []int
		var nFinal, nChanged, nRe, nTrig int64
		hist := map[string]int64{}
		for j, r := range rs {
			st = compat.AppendPlain(st, int(r.steps))
			if r.final {
				nFinal++
				continue
			}
			if r.changed {
				nChanged++
			}
			if j == 0 || r.trig == 0 {
				continue
			}
			nRe++
			nTrig += int64(r.ntrig)
			ch := "unchanged"
			if r.changed {
				ch = "changed"
			}
			hist[statMaskName(r.trig)+" "+ch]++
		}
		slices.Sort(st)
		mn, md, mx := 0, 0, 0
		if len(st) > 0 {
			mn, md, mx = st[0], st[len(st)/2], st[len(st)-1]
		}
		statLine("(d) func %s: %d steps, %d solves (%d final), steps min %d median %d max %d, %d re-solves, %d changed its summary, %d triggers per re-solve (x100)", compat.ArgS(p.Funcs[id].Name), compat.ArgI(statSteps[id]), compat.ArgI(int64(len(rs))), compat.ArgI(nFinal), compat.ArgI(int64(mn)), compat.ArgI(int64(md)), compat.ArgI(int64(mx)), compat.ArgI(nRe), compat.ArgI(nChanged), compat.ArgI(nTrig*100/nz(nRe)))
		for _, c := range statTop(hist, 8) {
			statLine("(d)     %s: %d", compat.ArgS(c.key), compat.ArgI(c.n))
		}
	}
}
