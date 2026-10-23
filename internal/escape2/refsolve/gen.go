package refsolve

import (
	"fmt"
	"math/rand"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// Case is one random input of the differential tests: a function (function 0
// of Prog) and the environment around it.
type Case struct {
	Seed int64
	F    *eir.Func
	Env  *MapEnv
}

// Dump describes the case for a failure message.
func (c *Case) Dump() string {
	s := fmt.Sprintf("seed %d: %d inputs, %d results, %d objects, %d blocks, %d loops\n",
		c.Seed, len(c.F.Inputs), c.F.Results, len(c.F.Objects), len(c.F.Blocks), len(c.F.Loops))
	for _, b := range c.F.Blocks {
		s += fmt.Sprintf("b%d preds=%v succs=%v loop=%d\n", b.ID, b.Preds, b.Succs, b.Loop)
		for _, in := range b.Instrs {
			s += fmt.Sprintf("  %s\n", dumpInstr(&in))
		}
	}
	for i, o := range c.F.Objects {
		s += fmt.Sprintf("obj %d: kind %d life %d loop %d\n", i, o.Kind, o.Life, o.Loop)
	}
	for id, sm := range c.Env.Sums {
		if sm != nil {
			s += fmt.Sprintf("summary of f%d:\n%s", id, Canon(c.Env.P, resultOf(sm)))
		}
	}
	for site, cs := range c.Env.Cands[0] {
		s += fmt.Sprintf("candidates of site %d: %v\n", site, cs)
	}
	return s
}

func dumpInstr(in *eir.Instr) string {
	s := fmt.Sprintf("@%d op%d dst=r%d x=r%d y=r%d args=%v obj=%d path=%v fields=%v idx=%d heap=%d sink=%d",
		in.Pos, in.Op, in.Dst, in.X, in.Y, in.Args, int32(in.Obj), in.Path, in.Fields, in.Index, in.Heap, in.Sink)
	if c := in.Call; c != nil {
		s += fmt.Sprintf(" call{kind=%d callee=%d contract=%q target=r%d args=%v results=%v site=%d}",
			c.Kind, int32(c.Callee), c.Contract, c.Target, c.Args, c.Results, c.Site)
	}
	return s
}

// Normalize makes sm's sets antichains in order, as solvers produce them.
func Normalize(ps domain.Paths, sm *summary.Summary) {
	(&solver{ps: ps}).antichains(sm)
}

type gen struct {
	rng *rand.Rand
	ps  domain.Paths
	b   *B
	st  types2.Type // struct{ f0, f1 unsafe.Pointer; f2 struct{ x, y unsafe.Pointer } }

	pool    []eir.Reg
	nCallee int
	callees []*eir.Func
	env     *MapEnv
}

func newStructType() types2.Type {
	up := types2.Typ[types2.UnsafePointer]
	fld := func(n string, t types2.Type) *types2.Var { return types2.NewField(0, nil, n, t, false) }
	inner := types2.NewStruct([]*types2.Var{fld("x", up), fld("y", up)}, nil)
	return types2.NewStruct([]*types2.Var{fld("f0", up), fld("f1", up), fld("f2", inner)}, nil)
}

// Random builds a random case from seed: a well-formed function (SSA,
// blocks in a chain with at most one loop, objects of every lifetime),
// callees with random summaries, and candidates for dynamic calls, all within
// the limits of the domain.
func Random(seed int64, ps domain.Paths) *Case {
	g := &gen{rng: rand.New(rand.NewSource(seed)), ps: ps, st: newStructType()}
	prog := &eir.Program{}
	g.env = &MapEnv{
		Prog: prog, P: ps,
		Sums:      map[eir.FuncID]*summary.Summary{},
		Cands:     map[eir.FuncID]map[eir.SiteID][]eir.FuncID{0: {}},
		Contracts: map[string]*summary.Summary{},
		HeapOnlyFn: func(t types2.Type, field int) bool {
			return field == 1
		},
		// Every other call site is deferred (D49): which ones the scheduler
		// defers does not matter for the two solvers' agreement.
		DeferredFn: func(f *eir.Func, c *eir.Call) bool {
			return c.Site%2 == 1
		},
	}
	nin, nres := g.rng.Intn(4), g.rng.Intn(3)
	g.b = NewFunc(0, nin, nres)
	prog.Funcs = append(prog.Funcs, g.b.F)
	g.nCallee = 1 + g.rng.Intn(3)
	for i := 1; i <= g.nCallee; i++ {
		c := &eir.Func{ID: eir.FuncID(i), Name: "callee", Results: g.rng.Intn(3), Bodyless: g.rng.Intn(4) == 0}
		c.Inputs = make([]eir.Input, 1+g.rng.Intn(3))
		prog.Funcs = append(prog.Funcs, c)
		g.callees = append(g.callees, c)
		if g.rng.Intn(5) != 0 {
			g.env.Sums[c.ID] = g.randSummary(len(c.Inputs), c.Results)
		}
	}
	g.env.Contracts["c0"] = g.randSummary(2, 1)
	g.body()
	return &Case{Seed: seed, F: g.b.F, Env: g.env}
}

// randFieldPath is a random field path of a result: fields and elements,
// never a load (a struct value's fields are not behind pointers).
func (g *gen) randFieldPath(max int) domain.PathID {
	p := domain.Root
	steps := []domain.Step{domain.StepElem, 0, 1}
	for i := g.rng.Intn(max + 1); i > 0; i-- {
		p, _ = g.ps.Extend(p, steps[g.rng.Intn(len(steps))])
	}
	return p
}

func (g *gen) randKey(inputs int) domain.Key {
	p, ge := domain.Root, g.rng.Intn(3) == 0
	steps := []domain.Step{domain.StepLoad, domain.StepElem, 0, 1}
	for i := g.rng.Intn(4); i > 0; i-- {
		var cut bool
		p, cut = g.ps.Extend(p, steps[g.rng.Intn(len(steps))])
		ge = ge || cut
	}
	f := domain.KeyFlags{GE: ge}
	if !ge {
		// Callee keys of every form: FieldsGE (pluto/fmt's walk and
		// stores through a GE tag make them) and Deep (the walk's loads).
		switch g.rng.Intn(8) {
		case 0:
			f.FieldsGE = true
		case 1:
			f.Deep = true
		}
	}
	return domain.MakeKey(g.rng.Intn(inputs), p, f)
}

func (g *gen) randSummary(inputs, results int) *summary.Summary {
	sm := &summary.Summary{Inputs: inputs, Res: make([]summary.Result, results)}
	for i := g.rng.Intn(3); i > 0; i-- {
		sm.Esc = append(sm.Esc, g.randKey(inputs))
	}
	for i := g.rng.Intn(3); i > 0; i-- {
		sm.Store = append(sm.Store, summary.StorePair{Src: g.randKey(inputs), Dst: g.randKey(inputs)})
	}
	for i := g.rng.Intn(2); i > 0; i-- {
		sm.HeapStore = append(sm.HeapStore, g.randKey(inputs))
	}
	for r := range sm.Res {
		for i := g.rng.Intn(3); i > 0; i-- {
			sm.Res[r].From = append(sm.Res[r].From, summary.ResultKey{Key: g.randKey(inputs), Field: g.randFieldPath(2), FieldGE: g.rng.Intn(4) == 0})
		}
		if g.rng.Intn(4) == 0 {
			sm.Res[r].Heap = []domain.PathID{g.randFieldPath(1)}
		}
		if g.rng.Intn(6) == 0 {
			sm.Res[r].CountedHeap = []domain.PathID{g.randFieldPath(1)}
		}
	}
	if results > 0 && g.rng.Intn(5) == 0 {
		sm.Region.Builds = true
		sm.Region.OuterResults = []int{g.rng.Intn(results)}
	}
	Normalize(g.ps, sm)
	return sm
}

func (g *gen) pick() eir.Reg {
	if len(g.pool) == 0 || g.rng.Intn(25) == 0 {
		return 0
	}
	return g.pool[g.rng.Intn(len(g.pool))]
}

func (g *gen) add(r eir.Reg) { g.pool = append(g.pool, r) }

func (g *gen) body() {
	b, rng := g.b, g.rng
	nblocks := 1 + rng.Intn(4)
	for i := 1; i < nblocks; i++ {
		b.NewBlock()
		b.Edge(eir.BlockID(i-1), eir.BlockID(i))
	}
	hdr, latch := -1, -1
	if nblocks > 1 && rng.Intn(2) == 0 {
		hdr = 1 + rng.Intn(nblocks-1)
		latch = hdr + rng.Intn(nblocks-hdr)
		b.Edge(eir.BlockID(latch), eir.BlockID(hdr))
		var ids []eir.BlockID
		for i := hdr; i <= latch; i++ {
			ids = append(ids, eir.BlockID(i))
		}
		b.Loop(-1, ids...)
	}
	for i := rng.Intn(5); i > 0; i-- {
		life := []eir.Lifetime{eir.LifeStmt, eir.LifeIter, eir.LifeBlock, eir.LifeFrame, eir.LifeFrame, eir.LifeCaller, eir.LifeHeap}[rng.Intn(7)]
		loop, kind := -1, eir.ObjVar
		if life == eir.LifeIter {
			if hdr < 0 {
				life = eir.LifeBlock
			} else {
				loop = 0
			}
		}
		if life == eir.LifeCaller {
			kind = eir.ObjRegion
		}
		b.Obj(kind, life, loop, g.st)
	}
	var phi *eir.Instr
	var phiBlock eir.BlockID
	var loopRegs []eir.Reg
	for bi := 0; bi < nblocks; bi++ {
		b.At(eir.BlockID(bi))
		if bi == hdr {
			a := g.pick()
			b.Emit(eir.Instr{Op: eir.OpPhi, Args: []eir.Reg{a, 0}}, true)
			phiBlock = eir.BlockID(bi)
			g.add(b.F.Blocks[bi].Instrs[0].Dst)
		}
		for n := 1 + rng.Intn(5); n > 0; n-- {
			before := len(g.pool)
			g.instr()
			if hdr >= 0 && bi >= hdr && bi <= latch {
				loopRegs = append(loopRegs, g.pool[before:]...)
			}
		}
	}
	if hdr >= 0 {
		phi = &b.F.Blocks[phiBlock].Instrs[0]
		phi.Args[1] = phi.Dst
		if len(loopRegs) > 0 {
			phi.Args[1] = loopRegs[rng.Intn(len(loopRegs))]
		}
	}
}

func (g *gen) obj() eir.ObjID {
	if len(g.b.F.Objects) == 0 {
		return g.b.Obj(eir.ObjVar, eir.LifeFrame, -1, g.st)
	}
	return eir.ObjID(g.rng.Intn(len(g.b.F.Objects)))
}

func (g *gen) path() []int {
	opts := [][]int{nil, {0}, {1}, {2}, {2, 0}, {2, 1}, {eir.ElemStep}, {0, eir.ElemStep}}
	return opts[g.rng.Intn(len(opts))]
}

func (g *gen) args(n int) []eir.Reg {
	var a []eir.Reg
	for i := 0; i < n; i++ {
		a = append(a, g.pick())
	}
	return a
}

// instr emits one random instruction in the current block.
func (g *gen) instr() {
	b, rng := g.b, g.rng
	switch rng.Intn(24) {
	case 0, 1:
		g.add(b.Addr(g.obj(), g.path()...))
	case 2:
		g.add(b.Heap())
	case 3:
		g.add(b.CHeap())
	case 4, 5:
		if n := len(b.F.Inputs); n > 0 {
			g.add(b.In(rng.Intn(n)))
		} else {
			g.add(b.Heap())
		}
	case 6, 7:
		g.add(b.Load(g.pick()))
	case 8, 9:
		b.Store(g.pick(), g.pick())
	case 10:
		path := g.path()
		if g.rng.Intn(8) == 0 {
			path = append(path, eir.AnyStep) // an unknown offset (unsafe.Add)
		}
		g.add(b.Emit(eir.Instr{Op: eir.OpField, X: g.pick(), Path: path, Type: g.st}, true))
	case 11:
		g.add(b.Elem(g.pick()))
	case 12:
		g.add(b.Select(g.pick(), g.path()...))
	case 13:
		n := 1 + rng.Intn(3)
		var fields [][]int
		for i := 0; i < n; i++ {
			fields = append(fields, g.path())
		}
		g.add(b.Compose(g.args(n), fields))
	case 14:
		g.add(b.Copy(g.pick()))
	case 15:
		o := eir.NoObj
		if rng.Intn(3) != 0 {
			o = g.obj()
		}
		g.add(b.Conv(g.pick(), o))
	case 16:
		// A closure of one of the callees: a call of it resolves to that
		// callee alone (rules.go, "Calls of function values").
		r := b.Closure(g.obj(), g.args(rng.Intn(3))...)
		b.F.Blocks[b.cur].Instrs[len(b.F.Blocks[b.cur].Instrs)-1].Callee = g.callees[rng.Intn(len(g.callees))].ID
		g.add(r)
	case 17:
		g.add(b.MethodVal(g.obj(), g.pick()))
	case 18:
		g.add(b.Assert(g.pick()))
	case 19:
		kinds := []eir.SinkKind{eir.SinkReturn, eir.SinkReturn, eir.SinkGlobal, eir.SinkSend, eir.SinkTask, eir.SinkExternal}
		k := kinds[rng.Intn(len(kinds))]
		idx := 0
		if k == eir.SinkReturn {
			if b.F.Results == 0 {
				k = eir.SinkGlobal
			} else {
				idx = rng.Intn(b.F.Results)
			}
		}
		b.Sink(k, g.pick(), idx)
	case 20:
		b.Drop(g.obj())
	default:
		g.call()
	}
}

func (g *gen) call() {
	b, rng := g.b, g.rng
	callee := g.callees[rng.Intn(len(g.callees))]
	c := eir.Call{Args: g.args(1 + rng.Intn(3))}
	nres := callee.Results
	switch rng.Intn(5) {
	case 0, 1:
		c.Kind, c.Callee = eir.CallStatic, callee.ID
	case 2:
		c.Kind, c.Target = eir.CallIface, g.pick()
	case 3:
		c.Kind, c.Target = eir.CallFunc, g.pick()
	case 4:
		if rng.Intn(2) == 0 {
			c.Kind, c.Contract, nres = eir.CallContract, "c0", 1
			c.Args = g.args(2)
		} else {
			c.Kind, c.Target = eir.CallTypeParam, g.pick()
		}
	}
	if c.Kind == eir.CallIface || c.Kind == eir.CallFunc || c.Kind == eir.CallTypeParam {
		nres = rng.Intn(3)
	}
	site := eir.SiteID(b.F.NumSites)
	rs := b.Call(c, nres)
	if c.Kind == eir.CallIface || c.Kind == eir.CallFunc || c.Kind == eir.CallTypeParam {
		var cs []eir.FuncID
		for _, x := range g.callees {
			if rng.Intn(2) == 0 {
				cs = append(cs, x.ID)
			}
		}
		g.env.Cands[0][site] = cs
	}
	for _, r := range rs {
		if rng.Intn(5) != 0 {
			g.add(r)
		}
	}
}
