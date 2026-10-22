package refsolve

import (
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// NaivePaths is a domain.Paths with no cleverness: a slice of step lists and
// a map from their text to the ID. Its cut rule keeps the longest valid
// prefix, which is enough for tests that do not compare cuts with another
// interner; the differential tests give both solvers the same Paths.
type NaivePaths struct {
	steps [][]domain.Step
	ids   map[string]domain.PathID
}

// NewNaivePaths returns a Paths holding only Root.
func NewNaivePaths() *NaivePaths {
	p := &NaivePaths{ids: map[string]domain.PathID{}}
	p.intern(nil)
	return p
}

func key(steps []domain.Step) string {
	var b strings.Builder
	for _, s := range steps {
		b.WriteString(strconv.Itoa(int(s)))
		b.WriteByte(',')
	}
	return b.String()
}

func (p *NaivePaths) intern(steps []domain.Step) domain.PathID {
	k := key(steps)
	if id, ok := p.ids[k]; ok {
		return id
	}
	id := domain.PathID(len(p.steps))
	p.steps = append(p.steps, append([]domain.Step(nil), steps...))
	p.ids[k] = id
	return id
}

// validPrefix returns the length of the longest prefix of steps within the
// limits of section 18.3.
func validPrefix(steps []domain.Step) int {
	loads, run := 0, 0
	for i, s := range steps {
		if i >= domain.MaxSteps {
			return i
		}
		if s == domain.StepLoad {
			loads++
			run = 0
			if loads > domain.MaxLoads {
				return i
			}
		} else {
			run++
			if run > domain.MaxFieldNest {
				return i
			}
		}
	}
	return len(steps)
}

// Extend implements domain.Paths.
func (p *NaivePaths) Extend(q domain.PathID, s domain.Step) (domain.PathID, bool) {
	steps := append(append([]domain.Step(nil), p.steps[q]...), s)
	if n := validPrefix(steps); n < len(steps) {
		return p.intern(steps[:n]), true
	}
	return p.intern(steps), false
}

// Steps implements domain.Paths.
func (p *NaivePaths) Steps(q domain.PathID) []domain.Step { return p.steps[q] }

// Loads implements domain.Paths.
func (p *NaivePaths) Loads(q domain.PathID) int {
	n := 0
	for _, s := range p.steps[q] {
		if s == domain.StepLoad {
			n++
		}
	}
	return n
}

// HasPrefix implements domain.Paths.
func (p *NaivePaths) HasPrefix(q, prefix domain.PathID) bool {
	a, b := p.steps[q], p.steps[prefix]
	if len(b) > len(a) {
		return false
	}
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Cut implements domain.Paths: q up to and including its second load.
func (p *NaivePaths) Cut(q domain.PathID) domain.PathID {
	loads := 0
	for i, s := range p.steps[q] {
		if s == domain.StepLoad {
			loads++
			if loads == 2 {
				return p.intern(p.steps[q][:i+1])
			}
		}
	}
	return q
}

// Len implements domain.Paths.
func (p *NaivePaths) Len() int { return len(p.steps) }

// MapEnv is a solve.Env backed by maps, for tests and the differential
// harness.
type MapEnv struct {
	Prog       *eir.Program
	P          domain.Paths
	Sums       map[eir.FuncID]*summary.Summary
	Cands      map[eir.FuncID]map[eir.SiteID][]eir.FuncID // by caller and site
	Contracts  map[string]*summary.Summary
	HeapOnlyFn func(t types2.Type, field int) bool
	// InstantiatedFn answers Instantiated; nil means every type parameter
	// is open.
	InstantiatedFn func(tp *types2.TypeParam) ([]types2.Type, bool)
	// DeferredFn answers Deferred; nil defers no call.
	DeferredFn func(f *eir.Func, c *eir.Call) bool
}

// Program implements solve.Env.
func (e *MapEnv) Program() *eir.Program { return e.Prog }

// Paths implements solve.Env.
func (e *MapEnv) Paths() domain.Paths { return e.P }

// Summary implements solve.Env.
func (e *MapEnv) Summary(id eir.FuncID) *summary.Summary { return e.Sums[id] }

// Candidates implements solve.Env.
func (e *MapEnv) Candidates(f *eir.Func, c *eir.Call) []eir.FuncID { return e.Cands[f.ID][c.Site] }

// Contract implements solve.Env.
func (e *MapEnv) Contract(name string) (*summary.Summary, bool) {
	s, ok := e.Contracts[name]
	return s, ok
}

// HeapOnly implements solve.Env.
func (e *MapEnv) HeapOnly(t types2.Type, field int) bool {
	return e.HeapOnlyFn != nil && e.HeapOnlyFn(t, field)
}

// Instantiated implements solve.Env: InstantiatedFn, or open (any type).
func (e *MapEnv) Instantiated(tp *types2.TypeParam) ([]types2.Type, bool) {
	if e.InstantiatedFn == nil {
		return nil, false
	}
	return e.InstantiatedFn(tp)
}

// CanRead implements solve.Env: every summary may be read.
func (e *MapEnv) CanRead(f *eir.Func, id eir.FuncID) bool { return true }

// Deferred implements solve.Env: DeferredFn, or no call deferred.
func (e *MapEnv) Deferred(f *eir.Func, c *eir.Call) bool {
	return e.DeferredFn != nil && e.DeferredFn(f, c)
}

var _ solve.Env = (*MapEnv)(nil)
