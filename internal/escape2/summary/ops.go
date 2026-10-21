package summary

import "github.com/matjam/plutonium/internal/escape2/domain"

// Leq reports whether s is no wider than t: every entry of s is covered by
// one of t. It is the post-fixed-point check of D44 (plan section (f)): a
// loaded summary t is accepted when the summary computed against it is Leq t.
//
// Interface stub; the solver's implementation replaces it.
func Leq(ps domain.Paths, s, t *Summary) bool {
	panic("escape2/summary: Leq is not built yet")
}

// Widen applies the caps: past MaxEscPerInput or MaxStorePerInput entries of
// one input, that input's keys are cut and marked GE, and the sets are made
// antichains again. The result covers s.
//
// Interface stub; the solver's implementation replaces it.
func Widen(ps domain.Paths, s *Summary) {
	panic("escape2/summary: Widen is not built yet")
}
