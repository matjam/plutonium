// Package refsolve is the naive reference solver of the escape analysis (plan
// docs/plans/2026-10-20-escape-redesign.md, section (g)). It applies the rules
// of internal/escape2/solve/rules.go, one small function per rule, to plain
// sets until no rule adds anything. It has no difference propagation, no
// cycle collapse, no hash-consing and no caps beyond those the rules state.
// Speed does not matter; each rule can be read against its line of the table.
//
// It exists to check internal/escape2/solve: the differential tests run both
// on the same input and require equal summaries and findings. A disagreement
// is a bug in one of them.
//
// The files: values.go (tokens and values), ops.go (the operations on values
// of the table), check.go (the store check and sinks), rules.go (one function
// per instruction), call.go (the call rule), live.go (liveness for OpDrop),
// and this file (the fixed point and the summary).
package refsolve

import (
	"fmt"

	"sort"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// widen applies the caps of the summary (the table's last paragraph). It is a
// variable so that the tests of this package can run before the real
// summary.Widen exists.
var widen = summary.Widen

// The bounds of one reference solve: a case past them is a defect (of the
// rules, the generator or the solver) to be reported with its seed, never a
// reason to grow without end.
const (
	MaxPasses = 10000
	MaxValues = 200_000
)

// size is the number of tagged tokens the state holds.
func (s *solver) size() int {
	n := 0
	for _, v := range s.regs {
		n += len(v)
	}
	for _, v := range s.places {
		n += len(v)
	}
	return n
}

// Solve has the signature of solve.Solve, without the workspace: the
// reference solver allocates freely. It applies every rule to every
// instruction until a whole pass adds nothing.
func Solve(f *eir.Func, env solve.Env) solve.Result {
	s := newSolver(f, env)
	for pass := 0; ; pass++ {
		if pass > MaxPasses {
			panic(fmt.Sprintf("escape2/refsolve: no fixed point after %d passes", MaxPasses))
		}
		if n := s.size(); n > MaxValues {
			panic(fmt.Sprintf("escape2/refsolve: %d tagged tokens, past %d", n, MaxValues))
		}
		s.changed = false
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				s.apply(b, &b.Instrs[i])
			}
		}
		s.resolve() // defer.go
		if !s.changed {
			break
		}
	}
	return s.result()
}

// result is the summary and the findings the fixed point holds.
func (s *solver) result() solve.Result {
	sm := &summary.Summary{Inputs: len(s.f.Inputs), Res: make([]summary.Result, len(s.res))}
	for k := range s.esc {
		sm.Esc = append(sm.Esc, k)
	}
	for p := range s.stores {
		sm.Store = append(sm.Store, p)
	}
	for k := range s.hstore {
		sm.HeapStore = append(sm.HeapStore, k)
	}
	for i, r := range s.res {
		for rk := range r.from {
			sm.Res[i].From = append(sm.Res[i].From, rk)
		}
		sm.Res[i].Heap = sortedPaths(r.heap)
		sm.Res[i].CountedHeap = sortedPaths(r.cheap)
	}
	for r := range s.outer {
		sm.Region.OuterResults = append(sm.Region.OuterResults, r)
	}
	sort.Ints(sm.Region.OuterResults)
	sm.Deferred = s.deferredOut()
	sm.Region.Builds = s.builds
	if s.builds || len(s.outer) > 0 {
		sm.Region.Lits = s.regionLitsOut()
		sm.Region.Self = s.regionSelfOut()
	}
	s.antichains(sm)
	widen(s.ps, sm)
	s.antichains(sm)

	var fs []solve.Finding
	for _, f := range s.findings {
		fs = append(fs, f)
	}
	sort.Slice(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Pos != b.Pos {
			return a.Pos < b.Pos
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Obj < b.Obj
	})
	return solve.Result{Summary: sm, Findings: fs}
}

func sortedPaths(m map[domain.PathID]bool) []domain.PathID {
	var out []domain.PathID
	for p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// antichains drops every entry another entry covers (domain.Covers), and
// sorts what is left by key: "every set is an antichain under Covers, sorted
// by key" (summary.Summary). A store pair is covered when both its keys are;
// a result key when its key is and its field path is the same.
func (s *solver) antichains(sm *summary.Summary) {
	sm.Esc = s.keyAntichain(sm.Esc)
	sm.HeapStore = s.keyAntichain(sm.HeapStore)
	var st []summary.StorePair
	for i, p := range sm.Store {
		covered := false
		for j, q := range sm.Store {
			if i != j && p != q && domain.Covers(s.ps, q.Src, p.Src) && domain.Covers(s.ps, q.Dst, p.Dst) {
				covered = true
			}
		}
		if !covered && !containsPair(st, p) {
			st = append(st, p)
		}
	}
	sort.Slice(st, func(i, j int) bool {
		if st[i].Src != st[j].Src {
			return st[i].Src < st[j].Src
		}
		return st[i].Dst < st[j].Dst
	})
	sm.Store = st
	for r := range sm.Res {
		var from []summary.ResultKey
		for i, p := range sm.Res[r].From {
			covered := false
			for j, q := range sm.Res[r].From {
				if i != j && p != q && q.Field == p.Field && domain.Covers(s.ps, q.Key, p.Key) {
					covered = true
				}
			}
			if !covered && !containsResKey(from, p) {
				from = append(from, p)
			}
		}
		sort.Slice(from, func(i, j int) bool {
			if from[i].Key != from[j].Key {
				return from[i].Key < from[j].Key
			}
			return from[i].Field < from[j].Field
		})
		sm.Res[r].From = from
	}
}

func (s *solver) keyAntichain(ks []domain.Key) []domain.Key {
	var out []domain.Key
	for i, k := range ks {
		covered := false
		for j, o := range ks {
			if i != j && o != k && domain.Covers(s.ps, o, k) {
				covered = true
			}
		}
		if !covered && !containsKey(out, k) {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func containsKey(ks []domain.Key, k domain.Key) bool {
	for _, x := range ks {
		if x == k {
			return true
		}
	}
	return false
}

func containsPair(ps []summary.StorePair, p summary.StorePair) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
}

func containsResKey(ps []summary.ResultKey, p summary.ResultKey) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
}
