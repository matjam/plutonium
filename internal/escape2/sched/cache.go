package sched

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// The hook of decision D44's checked summary cache (plan section (f)): the
// driver asks the cache for the summaries of an SCC before solving it. When
// it has them, they are installed and each function of the SCC is solved
// once against them; the SCC is accepted only if every result is
// summary.Leq its loaded summary (the loaded summaries are then a
// post-fixed point, so they cover every execution, however they were made)
// and the facts each solve read are those recorded with it (the inputs did
// not change). Otherwise the SCC is solved from bottom as without a cache.
//
// A summary from the file may be wider than the one a run without the cache
// computes. So that this never rejects a program, a function's findings are
// never reported from summaries that depend on the file: when there are
// findings, every SCC that is below a function with a finding in the call
// graph and that is tainted (accepted from the file, or reading a tainted
// summary) is solved again from bottom, callees first, and its findings
// replace those of the first pass.

// Cache is the checked summary cache (internal/escape2/cache).
type Cache interface {
	// Begin is called for every SCC, in the order the driver solves them,
	// after the SCCs it reads are final, and again for an SCC that is
	// redone. With load, it returns the loaded summaries of comp's
	// functions in comp's order, interned in v.Paths(), or nil when it has
	// none for some function. The cache owns nothing it returns: the driver
	// may keep the summaries as its own.
	Begin(v CacheView, comp []eir.FuncID, load bool) []*summary.Summary
	// Recorder returns a recorder of what one solve of id reads.
	Recorder(id eir.FuncID) Recorder
	// Solved notes the reads of the latest solve of id, to be saved with
	// its summary.
	Solved(id eir.FuncID, r Recorder)
	// Matches reports whether the reads of the checking solve of id are
	// those recorded with its loaded summary.
	Matches(id eir.FuncID, r Recorder) bool
}

// Recorder records what a solve reads of the stage B facts and the contract
// table: the inputs of a summary other than the function's EIR and the
// summaries of its callees.
type Recorder interface {
	HeapOnly(T types2.Type, field int, heap bool)
	Contract(name string, s *summary.Summary, ok bool)
	Instantiated(tp *types2.TypeParam, types []types2.Type, known bool)
	Methods(t types2.Type, m *types2.Func, ids []eir.FuncID)
	PointerWrappers(u types2.Type, types []types2.Type, known bool)
}

// CacheView is what the cache reads of the run.
type CacheView interface {
	Program() *eir.Program
	Paths() domain.Paths
	// Summary is the summary of a function scheduled no later than the
	// current SCC (final for earlier SCCs).
	Summary(id eir.FuncID) *summary.Summary
	// Candidates are the functions a dynamic call may reach (stage B).
	Candidates(f *eir.Func, c *eir.Call) []eir.FuncID
	// Deps are the functions whose summaries id reads.
	Deps(id eir.FuncID) []eir.FuncID
}

// CacheOutcome says what the cache did for an SCC.
type CacheOutcome uint8

const (
	CacheOff      CacheOutcome = iota // no cache
	CacheMissed                       // the file has no summary for some function: solved from bottom
	CacheAccepted                     // loaded and checked
	CacheRejected                     // loaded, the check failed: solved from bottom
	CacheRedone                       // solved again from bottom before reporting a finding
)

// cacheView is the view of the run the cache reads.
type cacheView struct {
	*env
	g *callGraph
}

func (v cacheView) Candidates(f *eir.Func, c *eir.Call) []eir.FuncID {
	return v.facts.Candidates(f, c)
}

func (v cacheView) Deps(id eir.FuncID) []eir.FuncID { return v.g.deps[id] }

// recordingEnv is the solver's environment with its reads of facts recorded.
type recordingEnv struct {
	*env
	rec Recorder
}

func (e recordingEnv) HeapOnly(T types2.Type, field int) bool {
	h := e.env.HeapOnly(T, field)
	e.rec.HeapOnly(T, field, h)
	return h
}

func (e recordingEnv) Instantiated(tp *types2.TypeParam) ([]types2.Type, bool) {
	types, known := e.env.Instantiated(tp)
	e.rec.Instantiated(tp, types, known)
	return types, known
}

func (e recordingEnv) Methods(t types2.Type, m *types2.Func) []eir.FuncID {
	ids := e.env.Methods(t, m)
	e.rec.Methods(t, m, ids)
	return ids
}

func (e recordingEnv) PointerWrappers(u types2.Type) ([]types2.Type, bool) {
	types, known := e.env.PointerWrappers(u)
	e.rec.PointerWrappers(u, types, known)
	return types, known
}

// Deferred is decided from the EIR, which the cache's digest covers.
func (e recordingEnv) Deferred(f *eir.Func, c *eir.Call) bool { return e.env.Deferred(f, c) }

// CanRead is decided by the call graph, from the EIR.
func (e recordingEnv) CanRead(f *eir.Func, id eir.FuncID) bool { return e.env.CanRead(f, id) }

func (e recordingEnv) Contract(name string) (*summary.Summary, bool) {
	s, ok := e.env.Contract(name)
	e.rec.Contract(name, s, ok)
	return s, ok
}

// redoSCCs returns, callees first, the SCCs to solve again from bottom
// before findings are reported: those that are tainted (accepted from the
// file, or reading a tainted summary) and that some function with a finding
// reads, directly or through others (its down-closure in the call graph,
// itself included). Every other SCC is either untainted, so solved exactly
// as without a cache, or read by no function with a finding.
func redoSCCs(comps [][]eir.FuncID, g *callGraph, inSCC []int32, fromFile []bool, findings [][]solve.Finding) []int {
	tainted := make([]bool, len(comps))
	anyTainted := false
	for ci, comp := range comps {
		tainted[ci] = fromFile[ci]
		for _, id := range comp {
			for _, d := range g.deps[id] {
				if tainted[inSCC[d]] {
					tainted[ci] = true
				}
			}
		}
		anyTainted = anyTainted || tainted[ci]
	}
	if !anyTainted {
		return nil
	}
	need := make([]bool, len(comps))
	for ci := len(comps) - 1; ci >= 0; ci-- {
		for _, id := range comps[ci] {
			if len(findings[id]) > 0 {
				need[ci] = true
			}
		}
		if !need[ci] {
			continue
		}
		for _, id := range comps[ci] {
			for _, d := range g.deps[id] {
				need[inSCC[d]] = true
			}
		}
	}
	var out []int
	for ci := range comps {
		if need[ci] && tainted[ci] {
			out = append(out, ci)
		}
	}
	return out
}
