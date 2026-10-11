package lower

import "github.com/matjam/plutonium/internal/ssa"

// Count elision across calls (language spec sections 18.7 and 18.8).
//
// A string, a shared, synced or weak reference, or a channel, borrowed from memory a
// call could change (a field reached through a pointer, a slice element, a
// package variable) is held for the rest of its statement: copied into a
// temporary and retained, so that a call that overwrites or frees its
// source cannot free what it refers to (hold, in strcount.go). Most such
// calls cannot release any reference at all; the optimiser's
// opt.ElideStringHolds knows which, once the whole program is lowered, and
// removes the retain and its releases for a hold whose calls all cannot.
// Lowering records here what it needs: each hold's retains and releases,
// and the calls emitted while the hold was active. Strings and boxes share
// one account of which calls may release: a box's last release runs drop
// glue that releases strings, and a string's frees its buffer through an
// allocator, whose Free could do anything.

// A holdRec is a hold being lowered.
type holdRec struct {
	tmp        *ownedTemp
	retains    []*ssa.Value
	releases   []*ssa.Value
	start, end int // the hold is active for callLog[start:end]
	closed     bool
}

// startHold records the hold of tmp, whose retains are the calls emitted
// since callLog[from].
func (s *fnState) startHold(tmp *ownedTemp, from int) {
	rec := &holdRec{tmp: tmp, start: len(s.callLog)}
	rec.retains = append(rec.retains, s.callLog[from:]...)
	s.strHolds = append(s.strHolds, rec)
	s.holdOf[tmp] = rec
}

// noteRelease records the calls emitted since callLog[from], which release
// the statement temporary tmp.
func (s *fnState) noteRelease(tmp *ownedTemp, from int) {
	calls := s.callLog[from:]
	for _, c := range calls {
		s.tempRelease[c] = true
	}
	if rec := s.holdOf[tmp]; rec != nil {
		rec.releases = append(rec.releases, calls...)
	}
}

// closeHolds ends the holds among slots, which a cleanup entry has just
// released at its end.
func (s *fnState) closeHolds(slots []countedSlot) {
	for _, c := range slots {
		if rec := s.holdOf[c.tmp]; c.tmp != nil && rec != nil && !rec.closed {
			rec.closed = true
			rec.end = len(s.callLog)
		}
	}
}

// recordHolds gives the function's holds to the optimiser. A hold whose
// temporary was handed over to a place is a real reference, not a
// protection, and is left alone.
func (s *fnState) recordHolds() {
	for _, rec := range s.strHolds {
		if rec.tmp.consumed || !rec.closed || len(rec.retains) == 0 {
			continue
		}
		own := map[*ssa.Value]bool{}
		for _, r := range rec.releases {
			own[r] = true
		}
		h := ssa.StrHold{Retains: rec.retains, Releases: rec.releases}
		for _, c := range s.callLog[rec.start:rec.end] {
			// Taking references, and dropping those of statement
			// temporaries, never frees what a hold protects: the
			// protected reference belongs to its source, and the
			// temporaries are dropped at the end of the statement,
			// after the held value's last use.
			if own[c] || s.tempRelease[c] || isRetain(c) {
				continue
			}
			h.Covered = append(h.Covered, c)
		}
		s.f.StrHolds = append(s.f.StrHolds, h)
	}
}

func isRetain(c *ssa.Value) bool {
	sym, ok := c.Aux.(*ssa.Sym)
	return ok && c.Op == ssa.OpCall && (sym.Name == "pluto/rt.strRetain" || sym.Name == "pluto/rt.boxRetain" || sym.Name == "pluto/rt.chanRetain")
}
