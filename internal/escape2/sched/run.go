package sched

import (
	"fmt"
	"sort"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// The parts of other packages the driver calls, as variables so that the
// driver's tests can run it with stand-ins.
var (
	solveFunc = solve.Solve
	leq       = summary.Leq
	newPaths  = domain.NewPaths
	newArena  = domain.NewArena
	newSets   = domain.NewSets
)

// facts is the stage B implementation of Facts.
type facts struct {
	p    *eir.Program
	tt   *typeTable
	inst *instClosure
	c    *cands
	ho   *heapOnly
	rule dispatchRule
}

// StageB computes the whole-program facts of p.
func StageB(p *eir.Program) Facts {
	return stageB(p)
}

func stageB(p *eir.Program) *facts {
	tt := newTypeTable()
	inst := newInstClosure(tt)
	inst.run(p)
	c := newCands(p, tt, inst)
	f := &facts{p: p, tt: tt, inst: inst, c: c, rule: unionRule{c}}
	f.ho = newHeapOnly(p, tt, inst, c)
	return f
}

// Candidates are the functions a dynamic call in f may reach, by the
// dispatch rule in force (union over the candidate set in stage 2).
func (f *facts) Candidates(fn *eir.Func, c *eir.Call) []eir.FuncID {
	return f.rule.callees(fn, c)
}

// HeapOnly reports whether field index field of struct type T holds only
// heap pointers (plan invariant I6).
func (f *facts) HeapOnly(T types2.Type, field int) bool {
	return f.ho.heapOnly(T, field)
}

// Instantiated reports the type arguments a type parameter is instantiated
// with anywhere in the program. For a type parameter whose instantiations
// cannot be bounded it returns nil; InstantiatedKnown tells the two apart.
func (f *facts) Instantiated(tp *types2.TypeParam) []types2.Type {
	types, _ := f.inst.instantiated(tp)
	return types
}

// InstantiatedKnown is Instantiated, and whether the set is known: false
// for an open type parameter, which may be instantiated with any type.
func (f *facts) InstantiatedKnown(tp *types2.TypeParam) ([]types2.Type, bool) {
	return f.inst.instantiated(tp)
}

// Options configure Run.
type Options struct {
	// Contract gives the summaries of contract-table entries; nil means
	// those of internal/escape2/contract.
	Contract func(name string) (*summary.Summary, bool)
	// OnSCC, if set, is called after each SCC is final, with its
	// functions and measurements, before its arena is reset.
	OnSCC func(scc []eir.FuncID, st SCCStats)
}

// SCCStats are the measurements of one SCC.
type SCCStats struct {
	Funcs      int
	Solves     int
	ArenaBytes int // the most the arena held while the SCC was solved
	SetBytes   int // the most the set table held
}

// Run analyses p: stage B, then the SCCs callees first.
func Run(p *eir.Program) *Result {
	return RunWith(p, Options{})
}

// maxSolvesPerFunc bounds the solves of one function within its SCC. The
// domain is finite and the transfer monotone, so the bound is never reached
// by a correct solver; reaching it is an internal compiler error, never a
// silent stop.
const maxSolvesPerFunc = 10000

// RunWith is Run with options.
func RunWith(p *eir.Program, opts Options) *Result {
	f := stageB(p)
	g := buildGraph(p, f.rule)
	comps := g.sccs()

	env := &env{
		p:       p,
		paths:   newPaths(),
		facts:   f,
		sums:    make([]*summary.Summary, len(p.Funcs)),
		inSCC:   make([]int32, len(p.Funcs)),
		opts:    opts,
		current: -1,
	}
	if env.opts.Contract == nil {
		env.opts.Contract = newContractSummaries(env.paths).lookup
	}
	for i := range env.inSCC {
		env.inSCC[i] = -1
	}
	arena := newArena()
	ws := &solve.Workspace{Arena: arena, Sets: newSets(arena)}

	res := &Result{Summaries: env.sums}
	res.Stats.Funcs = len(p.Funcs)
	res.Stats.SCCs = len(comps)

	// readers[g] are the functions of g's SCC that read g's summary.
	readers := make([][]eir.FuncID, len(p.Funcs))
	findings := make([][]solve.Finding, len(p.Funcs))
	for ci, comp := range comps {
		if len(comp) > res.Stats.LargestSCC {
			res.Stats.LargestSCC = len(comp)
		}
		for _, id := range comp {
			env.inSCC[id] = int32(ci)
		}
		for _, id := range comp {
			for _, d := range g.deps[id] {
				if env.inSCC[d] == int32(ci) {
					readers[d] = append(readers[d], id)
				}
			}
		}
		env.current = int32(ci)

		var st SCCStats
		st.Funcs = len(comp)
		work := append([]eir.FuncID(nil), comp...)
		queued := map[eir.FuncID]bool{}
		for _, id := range comp {
			queued[id] = true
		}
		solves := map[eir.FuncID]int{}
		for len(work) > 0 {
			id := work[0]
			work = work[1:]
			queued[id] = false
			solves[id]++
			if solves[id] > maxSolvesPerFunc {
				panic(fmt.Sprintf("internal compiler error: escape2: %s did not converge in its SCC of %d functions", p.Funcs[id].Name, len(comp)))
			}
			r := solveFunc(p.Funcs[id], env, ws)
			st.Solves++
			findings[id] = r.Findings
			prev := env.sums[id]
			env.sums[id] = r.Summary
			if b := arena.Bytes(); b > st.ArenaBytes {
				st.ArenaBytes = b
			}
			if b := ws.Sets.Bytes(); b > st.SetBytes {
				st.SetBytes = b
			}
			if prev != nil && r.Summary != nil && leq(env.paths, r.Summary, prev) {
				continue
			}
			for _, rd := range readers[id] {
				if !queued[rd] {
					queued[rd] = true
					work = append(work, rd)
				}
			}
		}

		// The SCC is final: copy its summaries out of anything the arena
		// may share, report its findings, then reset the arena.
		for _, id := range comp {
			env.sums[id] = clone(env.sums[id])
			res.Findings = append(res.Findings, findings[id]...)
			findings[id] = nil
			readers[id] = nil
			res.Stats.SummaryBytes += summaryBytes(env.sums[id])
		}
		res.Stats.Iterations += st.Solves
		if b := st.ArenaBytes + st.SetBytes; b > res.Stats.PeakArena {
			res.Stats.PeakArena = b
		}
		if opts.OnSCC != nil {
			opts.OnSCC(comp, st)
		}
		ws.Sets.Reset()
		arena.Reset()
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.Pos != b.Pos {
			return a.Pos < b.Pos
		}
		if a.Func != b.Func {
			return a.Func < b.Func
		}
		return a.Code < b.Code
	})
	return res
}

// env is what the solver reads of the rest of the program (solve.Env).
type env struct {
	p       *eir.Program
	paths   domain.Paths
	facts   *facts
	sums    []*summary.Summary
	inSCC   []int32
	current int32
	opts    Options
}

func (e *env) Program() *eir.Program { return e.p }
func (e *env) Paths() domain.Paths   { return e.paths }

// Summary is the current summary of a callee: final for earlier SCCs, the
// latest iterate in the current one, nil (bottom) before the first.
func (e *env) Summary(id eir.FuncID) *summary.Summary {
	if int(id) >= len(e.sums) {
		return nil
	}
	if e.inSCC[id] < 0 || e.inSCC[id] > e.current {
		// Not scheduled before the current SCC: the graph has no edge to
		// it, so the solver must not depend on it.
		panic(fmt.Sprintf("internal compiler error: escape2: the summary of %s was read before it was scheduled", e.p.Funcs[id].Name))
	}
	return e.sums[id]
}

func (e *env) Candidates(f *eir.Func, c *eir.Call) []eir.FuncID {
	return e.facts.Candidates(f, c)
}

func (e *env) Contract(name string) (*summary.Summary, bool) {
	if e.opts.Contract == nil {
		return nil, false
	}
	return e.opts.Contract(name)
}

func (e *env) HeapOnly(T types2.Type, field int) bool { return e.facts.HeapOnly(T, field) }

// clone copies a summary into memory of its own, sharing nothing with the
// solver's workspace.
func clone(s *summary.Summary) *summary.Summary {
	if s == nil {
		return nil
	}
	c := &summary.Summary{
		Inputs:    s.Inputs,
		Esc:       append([]domain.Key(nil), s.Esc...),
		Store:     append([]summary.StorePair(nil), s.Store...),
		HeapStore: append([]domain.Key(nil), s.HeapStore...),
		Region: summary.Region{
			Builds:       s.Region.Builds,
			OuterResults: append([]int(nil), s.Region.OuterResults...),
		},
		Reasons: append([]summary.Reason(nil), s.Reasons...),
	}
	if s.Res != nil {
		c.Res = make([]summary.Result, len(s.Res))
		for i, r := range s.Res {
			c.Res[i] = summary.Result{
				From:        append([]summary.ResultKey(nil), r.From...),
				Heap:        append([]domain.PathID(nil), r.Heap...),
				CountedHeap: append([]domain.PathID(nil), r.CountedHeap...),
			}
		}
	}
	return c
}

// summaryBytes estimates the memory a summary holds.
func summaryBytes(s *summary.Summary) int {
	if s == nil {
		return 0
	}
	n := 128 + 8*len(s.Esc) + 16*len(s.Store) + 8*len(s.HeapStore) + 8*len(s.Region.OuterResults) + 12*len(s.Reasons)
	for _, r := range s.Res {
		n += 56 + 16*len(r.From) + 4*len(r.Heap) + 4*len(r.CountedHeap)
	}
	return n
}
