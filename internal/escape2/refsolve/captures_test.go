package refsolve

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
)

// capturingLiteral is a function literal of three captures (its record,
// input 0, holds their addresses at fields 0 to 2): it keeps a pointer three
// loads deep in capture 0 (an argument evaluator's call site: e.f.n), and
// reads capture 1 (a sibling) and capture 2 (the literal itself, as a
// recursive closure holds its own variable). With cap, the loads of the
// captures' addresses from the record are StepCap loads (eir.Instr.Cap),
// as the builder makes them.
func capturingLiteral(cap bool) *B {
	b := NewFunc(0, 1, 1)
	load := b.Load
	if cap {
		load = b.LoadCap
	}
	rec := b.In(0)
	e := b.Load(load(b.Field(rec, 0)))
	f := b.Load(b.Field(e, 0))
	n := b.Load(b.Field(f, 0))
	b.Sink(eir.SinkGlobal, n, 0)
	b.Sink(eir.SinkReturn, b.Load(load(b.Field(rec, 1))), 0)
	self := b.Load(load(b.Field(rec, 2)))
	_ = self
	return b
}

// TestCaptureLoadsUncounted: reading a captured variable is not a load the
// limits of section 18.3 count. The literal's key of what it keeps is the
// exact path through capture 0, three loads deep after the capture's own,
// in both solvers; it covers neither the sibling capture nor the literal's
// own variable. Counted as a load, the same path is past MaxLoads and
// widened.
func TestCaptureLoadsUncounted(t *testing.T) {
	b := capturingLiteral(true)
	e, ps := env(b.F)
	got, r := run(t, b.F, e)
	has(t, got, "esc in0.f0.^*f0.*f0.*\n")
	for _, k := range r.Summary.Esc {
		for _, sib := range []domain.Key{capKey(ps, 1), capKey(ps, 2)} {
			if domain.Covers(ps, k, sib) {
				t.Errorf("%s covers the capture %s", CanonKey(ps, k), CanonKey(ps, sib))
			}
		}
	}
	if d := Compare(&Case{F: b.F, Env: &MapEnv{Prog: e.Prog, P: domain.NewPaths(), Sums: e.Sums, Cands: e.Cands, Contracts: e.Contracts}}); d != "" {
		t.Errorf("solve and refsolve differ:\n%s", d)
	}

	old := capturingLiteral(false)
	e, _ = env(old.F)
	got, _ = run(t, old.F, e)
	has(t, got, "esc in0.f0.**f0.**\n") // Deep on the path cut after its second load
	hasNot(t, got, "esc in0.f0.**f0.*f0.*\n")
}

// capKey is the key of capture i's own address in a literal's record
// (input 0): the record's field i loaded.
func capKey(ps domain.Paths, i int) domain.Key {
	p, _ := ps.Extend(domain.Root, domain.Step(i))
	p, _ = ps.Extend(p, domain.StepCap)
	return domain.MakeKey(0, p, domain.KeyFlags{})
}
