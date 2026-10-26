package sched

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// A function whose solve passes the solver's implementation limits
// (solve.MaxFuncArena, solve.MaxFuncSteps) is an error of that function,
// E3010, and the analysis goes on: the function is given the top summary,
// which says it does everything a function can do with what it is passed,
// so every caller is checked against the worst case and nothing it does is
// accepted on faith. Its own frame is not checked (that is what the error
// says); the rest of the program is.

// codeEscapeLimit is the code of a function past the solver's limits.
const codeEscapeLimit = diag.EscapeLimit

// solveGuarded is solveFunc, turning a solve past the limits (Result.Limit)
// into the top summary and an E3010 finding at the function.
func solveGuarded(f *eir.Func, env solve.Env, ws *solve.Workspace) solve.Result {
	r := solveFunc(f, env, ws)
	if r.Limit != nil {
		r = limitResult(f, env)
	}
	return r
}

// limitResult is the result of a function past the solver's limits: the
// top summary and E3010 at the function.
func limitResult(f *eir.Func, env solve.Env) solve.Result {
	return solve.Result{
		Summary: topSummary(env.Paths(), f),
		Findings: []solve.Finding{{
			Code:   codeEscapeLimit,
			Pos:    f.Pos,
			Func:   f.ID,
			Obj:    eir.NoObj,
			Callee: eir.NoFunc,
			Entry:  -1,
		}},
	}
}

// topSummary is the summary of a function that may do anything with what
// it is passed: every input, and everything reachable from it, escapes;
// every input may be stored into memory every input leads to, and filled
// with heap pointers; every result may hold every input and heap pointers;
// it may build closures in the region its caller provides, returned in any
// result. Its keys carry no flags: each pointer is kept as a view (not as
// a counted or borrowed reference), the strongest effect a key states, so
// using it is sound whatever the function does.
func topSummary(ps domain.Paths, f *eir.Func) *summary.Summary {
	n := len(f.Inputs)
	if n > domain.MaxInputs {
		n = domain.MaxInputs
	}
	s := &summary.Summary{Inputs: n}
	all := domain.KeyFlags{GE: true}
	for i := 0; i < n; i++ {
		k := domain.MakeKey(i, domain.Root, all)
		// Kept anywhere, a package variable or a task included (Strict).
		s.Esc = append(s.Esc, k.WithStrict())
		s.HeapStore = append(s.HeapStore, k)
		for j := 0; j < n; j++ {
			s.Store = append(s.Store, summary.StorePair{Src: k, Dst: domain.MakeKey(j, domain.Root, all)})
		}
	}
	for r := 0; r < f.Results; r++ {
		res := summary.Result{Heap: []domain.PathID{domain.Root}, CountedHeap: []domain.PathID{domain.Root}}
		for i := 0; i < n; i++ {
			res.From = append(res.From, summary.ResultKey{Key: domain.MakeKey(i, domain.Root, all), Field: domain.Root, FieldGE: true})
		}
		s.Res = append(s.Res, res)
		s.Region.OuterResults = append(s.Region.OuterResults, r)
	}
	s.Region.Builds = true
	s.Region.Self = true
	s.Region.Lits = []eir.FuncID{eir.NoFunc} // closures it cannot name
	summary.Widen(ps, s)
	return s
}
