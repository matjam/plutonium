package sched

import (
	"github.com/matjam/plutonium/internal/escape2/contract"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// contractSummaries turns the entries of the contract table
// (internal/escape2/contract) into summaries, once per entry per run: the
// summary states exactly the entry's effect, a Ref (argument Arg loaded
// Deref times) being the key of input Arg at a path of Deref loads.
type contractSummaries struct {
	paths domain.Paths
	memo  map[string]*summary.Summary
}

func newContractSummaries(ps domain.Paths) *contractSummaries {
	return &contractSummaries{paths: ps, memo: map[string]*summary.Summary{}}
}

func (c *contractSummaries) lookup(name string) (*summary.Summary, bool) {
	if s, ok := c.memo[name]; ok {
		return s, true
	}
	e, ok := contract.Lookup(name)
	if !ok {
		return nil, false
	}
	s := c.summarize(e.Effect)
	c.memo[name] = s
	return s, true
}

func (c *contractSummaries) key(r contract.Ref) domain.Key {
	p, ge := domain.Root, false
	for i := 0; i < r.Deref; i++ {
		var cut bool
		p, cut = c.paths.Extend(p, domain.StepLoad)
		ge = ge || cut
	}
	return domain.MakeKey(r.Arg, p, domain.KeyFlags{GE: ge})
}

func (c *contractSummaries) summarize(eff contract.Effect) *summary.Summary {
	s := &summary.Summary{}
	inputs := 0
	note := func(r contract.Ref) {
		if r.Arg+1 > inputs {
			inputs = r.Arg + 1
		}
	}
	for _, r := range eff.Escapes {
		note(r)
		// Kept by a task or the runtime, where no lifetime check
		// follows it (domain.KeyFlags.Strict).
		s.Esc = append(s.Esc, c.key(r).WithStrict())
	}
	for _, st := range eff.Stores {
		note(st.Src)
		note(st.Dst)
		s.Store = append(s.Store, summary.StorePair{Src: c.key(st.Src), Dst: c.key(st.Dst)})
	}
	n := len(eff.Results)
	if len(eff.Heap) > n {
		n = len(eff.Heap)
	}
	if n > 0 {
		s.Res = make([]summary.Result, n)
	}
	for i, refs := range eff.Results {
		for _, r := range refs {
			note(r)
			s.Res[i].From = append(s.Res[i].From, summary.ResultKey{Key: c.key(r), Field: domain.Root})
		}
	}
	for i, h := range eff.Heap {
		if h {
			s.Res[i].Heap = []domain.PathID{domain.Root}
		}
	}
	s.Inputs = inputs
	// The summary format keeps sets sorted antichains.
	summary.Widen(c.paths, s)
	return s
}
