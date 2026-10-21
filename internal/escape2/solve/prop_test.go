package solve

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/escape2/summary/summarytest"
)

// randKey makes a random key of one of n inputs.
func randKey(rng *rand.Rand, e *testEnv, n int) domain.Key {
	var steps []string
	for range rng.IntN(4) {
		switch rng.IntN(4) {
		case 0:
			steps = append(steps, "*")
		case 1:
			steps = append(steps, "[]")
		default:
			steps = append(steps, []string{"0", "1"}[rng.IntN(2)])
		}
	}
	return e.key(rng.IntN(n), rng.IntN(5) == 0, steps...)
}

// randSummary makes a random summary of a function with n inputs and r
// results; extra adds entries to base instead (a wider summary).
func randSummary(rng *rand.Rand, e *testEnv, n, r int, base *summary.Summary) *summary.Summary {
	s := &summary.Summary{Inputs: n, Res: make([]summary.Result, r)}
	if base != nil {
		s.Esc = slices.Clone(base.Esc)
		s.Store = slices.Clone(base.Store)
		s.HeapStore = slices.Clone(base.HeapStore)
		for i := range base.Res {
			s.Res[i].From = slices.Clone(base.Res[i].From)
			s.Res[i].Heap = slices.Clone(base.Res[i].Heap)
		}
		s.Region.OuterResults = slices.Clone(base.Region.OuterResults)
	}
	for range rng.IntN(3) {
		s.Esc = append(s.Esc, randKey(rng, e, n))
	}
	for range rng.IntN(3) {
		s.Store = append(s.Store, summary.StorePair{Src: randKey(rng, e, n), Dst: randKey(rng, e, n)})
	}
	if rng.IntN(4) == 0 {
		s.HeapStore = append(s.HeapStore, randKey(rng, e, n))
	}
	for i := range r {
		for range rng.IntN(2) {
			s.Res[i].From = append(s.Res[i].From, summary.ResultKey{Key: randKey(rng, e, n), Field: e.paths.Of(domain.Step(rng.IntN(2)))})
		}
		if rng.IntN(3) == 0 {
			s.Res[i].Heap = append(s.Res[i].Heap, domain.Root)
		}
		if rng.IntN(6) == 0 {
			s.Region.OuterResults = append(s.Region.OuterResults, i)
		}
	}
	summary.Widen(e.paths, s)
	return s
}

// randFunc makes a random function calling the callee g (2 inputs, 1
// result), with loops, statement temporaries and every instruction kind the
// solver knows, in one loop body block so that back edges matter.
func randFunc(rng *rand.Rand, e *testEnv, g *eir.Func) *fb {
	b := e.fn("rand", 2, 1)
	b.f.Loops = []eir.Loop{{Header: 1, Blocks: []eir.BlockID{1}, Parent: -1}}
	entry := b.cur
	body := b.block()
	exit := b.block()
	body.Loop = 0
	entry.Succs = []eir.BlockID{1}
	body.Preds = []eir.BlockID{0, 1}
	body.Succs = []eir.BlockID{1, 2}
	exit.Preds = []eir.BlockID{1}
	lives := []eir.Lifetime{eir.LifeFrame, eir.LifeIter, eir.LifeStmt, eir.LifeFrame, eir.LifeCaller}
	var objs []eir.ObjID
	for i := range 4 {
		l := lives[rng.IntN(len(lives))]
		loop := -1
		if l == eir.LifeIter {
			loop = 0
		}
		objs = append(objs, b.obj("o"+string(rune('a'+i)), l, loop))
	}
	b.cur = body
	phi := b.reg()
	b.emit(eir.Instr{Op: eir.OpPhi, Dst: phi})
	phiAt := len(body.Instrs) - 1
	regs := []eir.Reg{b.input(0), b.input(1)}
	pick := func() eir.Reg { return regs[rng.IntN(len(regs))] }
	path := func() []int {
		var p []int
		for range rng.IntN(4) {
			p = append(p, []int{0, 1, eir.ElemStep}[rng.IntN(3)])
		}
		return p
	}
	for i := range 12 + rng.IntN(20) {
		b.at(100 + i)
		var r eir.Reg
		switch rng.IntN(13) {
		case 0:
			r = b.addr(objs[rng.IntN(len(objs))], path()...)
		case 1:
			r = b.heap()
		case 2:
			r = b.load(pick())
		case 3:
			b.store(pick(), pick())
		case 4:
			r = b.field(pick(), path()...)
		case 5:
			r = b.sel(pick(), path()...)
		case 6:
			r = b.compose([][]int{{0}, {1}}, pick(), pick())
		case 7:
			r = b.copyOf(pick())
		case 8:
			b.sink([]eir.SinkKind{eir.SinkGlobal, eir.SinkSend, eir.SinkTask}[rng.IntN(3)], pick())
		case 9:
			b.ret(0, pick())
		case 10:
			r = b.static(g, 1, pick(), pick())[0]
		case 11:
			o := objs[rng.IntN(len(objs))]
			r = b.reg()
			b.emit(eir.Instr{Op: eir.OpConv, Dst: r, X: pick(), Obj: o})
		case 12:
			b.emit(eir.Instr{Op: eir.OpDrop, Obj: objs[rng.IntN(len(objs))]})
		}
		if r != 0 {
			regs = append(regs, r)
		}
	}
	body.Instrs[phiAt].Args = []eir.Reg{regs[0], pick()}
	b.cur = exit
	b.ret(0, pick())
	return b
}

func solveWith(e *testEnv, fn *eir.Func, noCollapse bool) Result {
	ws := &Workspace{Arena: &summarytest.Arena{}, Sets: summarytest.NewSets()}
	s := newSolver(fn, e, ws)
	s.noCollapse = noCollapse
	s.build()
	s.run()
	s.checkDrops()
	return Result{Summary: s.summary(), Findings: s.findings}
}

// Cycle collapse and the order of propagation do not change the result.
func TestPropCollapse(t *testing.T) {
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, 1))
		e := newEnv()
		g := e.fn("g", 2, 1)
		e.sums[g.f.ID] = randSummary(rng, e, 2, 1, nil)
		b := randFunc(rng, e, g.f)
		r1 := solveWith(e, b.f, false)
		r2 := solveWith(e, b.f, true)
		r3 := solveWith(e, b.f, false)
		if e.describe(r1.Summary) != e.describe(r2.Summary) || findings(r1) != findings(r2) {
			t.Fatalf("seed %d: with collapse\n%s\n%s\nwithout\n%s\n%s", seed, e.describe(r1.Summary), findings(r1), e.describe(r2.Summary), findings(r2))
		}
		if e.describe(r1.Summary) != e.describe(r3.Summary) || findings(r1) != findings(r3) {
			t.Fatalf("seed %d: not deterministic", seed)
		}
		for _, fd := range r1.Findings {
			ws := &Workspace{Arena: &summarytest.Arena{}, Sets: summarytest.NewSets()}
			w := Explain(b.f, e, ws, fd)
			if fd.Pos.IsValid() && (len(w) == 0 || w[len(w)-1] != fd.Pos) {
				t.Fatalf("seed %d: witness %v of a finding at %d", seed, w, fd.Pos)
			}
		}
	}
}

// Solving is monotone in the callees' summaries: a wider callee summary
// gives a wider summary and a superset of findings. This is what makes the
// SCC iteration converge, and what D44's check relies on (plan section (f)).
func TestPropMonotone(t *testing.T) {
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, 2))
		e := newEnv()
		g := e.fn("g", 2, 1)
		small := randSummary(rng, e, 2, 1, nil)
		big := randSummary(rng, e, 2, 1, small)
		if !summary.Leq(e.paths, small, big) {
			t.Fatalf("seed %d: generator: small not Leq big", seed)
		}
		b := randFunc(rng, e, g.f)
		e.sums[g.f.ID] = small
		r1 := solveWith(e, b.f, false)
		e.sums[g.f.ID] = big
		r2 := solveWith(e, b.f, false)
		if !summary.Leq(e.paths, r1.Summary, r2.Summary) {
			t.Fatalf("seed %d: summary not monotone\nsmall: %s\nbig:   %s", seed, e.describe(r1.Summary), e.describe(r2.Summary))
		}
		for _, f := range strings.Fields(findings(r1)) {
			if !slices.Contains(strings.Fields(findings(r2)), f) {
				t.Fatalf("seed %d: finding %s lost with a wider callee\n%s\n%s", seed, f, findings(r1), findings(r2))
			}
		}
	}
}

// Every frame object reaching a sink directly is reported (plan invariant
// I5), whatever else the function does: a check that no rule loses a token
// on the way to a sink.
func TestPropSinkReported(t *testing.T) {
	for seed := range uint64(200) {
		rng := rand.New(rand.NewPCG(seed, 3))
		e := newEnv()
		g := e.fn("g", 2, 1)
		e.sums[g.f.ID] = randSummary(rng, e, 2, 1, nil)
		b := randFunc(rng, e, g.f)
		x := b.obj("x", eir.LifeFrame, -1)
		s := b.obj("s", eir.LifeFrame, -1)
		// &x reaches package memory through a frame variable and a copy.
		b.cur = b.f.Blocks[1]
		b.store(b.addr(s, 1), b.addr(x))
		v := b.copyOf(b.load(b.addr(s)))
		b.at(999).sink(eir.SinkGlobal, b.sel(v, 1))
		r := solveWith(e, b.f, false)
		if !strings.Contains(findings(r), "E3002@999") {
			t.Fatalf("seed %d: lost: %s", seed, findings(r))
		}
	}
}

// realPathsEnv is a test environment on the domain's own path interner,
// whose cuts are what make solving non-monotone (summary.Join).
type realPathsEnv struct {
	*testEnv
	ps domain.Paths
}

func (e realPathsEnv) Paths() domain.Paths { return e.ps }

// maxSCCIterations bounds the iteration of a recursive function's summary.
// The order Join climbs is finite; the bound is what the property checks
// against, far above what the random functions need.
const maxSCCIterations = 200

// The SCC iteration s = Join(s, Solve(f with s)) reaches a post-fixed point
// within a bound, on recursive functions, with the domain's own paths. This
// is the iteration the scheduler runs (sched.RunWith).
func TestPropSCCFixedPoint(t *testing.T) {
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, 4))
		e := newEnv()
		env := realPathsEnv{e, domain.NewPaths()}
		g := e.fn("g", 2, 1)
		b := randFunc(rng, e, g.f)
		// Make every call of g a call of the function itself.
		for _, blk := range b.f.Blocks {
			for i := range blk.Instrs {
				if c := blk.Instrs[i].Call; c != nil && c.Callee == g.f.ID {
					c.Callee = b.f.ID
				}
			}
		}
		var s *summary.Summary
		n := 0
		for ; n < maxSCCIterations; n++ {
			e.sums[b.f.ID] = s
			arena := domain.NewArena()
			r := Solve(b.f, env, &Workspace{Arena: arena, Sets: domain.NewSets(arena)})
			if s != nil && summary.Leq(env.ps, r.Summary, s) {
				break
			}
			s = summary.Join(env.ps, s, r.Summary)
		}
		if n == maxSCCIterations {
			t.Fatalf("seed %d: no fixed point in %d iterations", seed, n)
		}
	}
}
