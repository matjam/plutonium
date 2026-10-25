// Package summarytest is a plain implementation of the domain interfaces
// (internal/escape2/domain) for the tests of the summary and solve packages,
// so that they do not wait for, or depend on the details of, the domain's own
// implementation. It follows the contracts of domain/api.go and nothing more;
// it is not tuned and keeps everything in Go maps and slices.
package summarytest

import (
	"fmt"
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/escape2/domain"
)

// Paths is a simple path interner.
type Paths struct {
	steps   [][]domain.Step
	index   map[string]domain.PathID
	filters domain.Filters
}

// Filters is the paths' table of type filters.
func (p *Paths) Filters() *domain.Filters { return &p.filters }

// NewPaths returns an interner holding only Root.
func NewPaths() *Paths {
	p := &Paths{index: map[string]domain.PathID{}}
	p.intern(nil)
	return p
}

func keyOf(s []domain.Step) string {
	var b strings.Builder
	for _, x := range s {
		fmt.Fprintf(&b, "%d,", x)
	}
	return b.String()
}

func (p *Paths) intern(s []domain.Step) domain.PathID {
	k := keyOf(s)
	if id, ok := p.index[k]; ok {
		return id
	}
	id := domain.PathID(len(p.steps))
	p.steps = append(p.steps, slices.Clone(s))
	p.index[k] = id
	return id
}

// Of interns the path of steps.
func (p *Paths) Of(steps ...domain.Step) domain.PathID {
	id := domain.Root
	for _, s := range steps {
		id, _ = p.Extend(id, s)
	}
	return id
}

func within(s []domain.Step) bool {
	loads, nest := 0, 0
	for _, x := range s {
		if x == domain.StepLoad {
			loads++
			nest = 0
		} else {
			nest++
		}
		if nest > domain.MaxFieldNest {
			return false
		}
	}
	return loads <= domain.MaxLoads && len(s) <= domain.MaxSteps
}

// Extend returns p followed by s, cut to the limits.
func (p *Paths) Extend(id domain.PathID, s domain.Step) (domain.PathID, bool) {
	old := p.steps[id]
	n := append(slices.Clone(old), s)
	if within(n) {
		return p.intern(n), false
	}
	// A field step past the nesting limit: p itself, which with GE covers
	// p+s. A load past the limits: the cut of p+s.
	if s != domain.StepLoad {
		return id, true
	}
	return p.Cut(p.intern(n[:len(n)-1])), true
}

// Steps returns the steps of id.
func (p *Paths) Steps(id domain.PathID) []domain.Step { return p.steps[id] }

// Loads counts the loads of id.
func (p *Paths) Loads(id domain.PathID) int {
	n := 0
	for _, s := range p.steps[id] {
		if s == domain.StepLoad {
			n++
		}
	}
	return n
}

// HasPrefix reports whether prefix is a prefix of id.
func (p *Paths) HasPrefix(id, prefix domain.PathID) bool {
	a, b := p.steps[id], p.steps[prefix]
	return len(b) <= len(a) && slices.Equal(a[:len(b)], b)
}

// Cut returns the prefix of id through its first load, or id itself when it
// has no load (domain.Paths.Cut): with GE it covers id.
func (p *Paths) Cut(id domain.PathID) domain.PathID {
	s := p.steps[id]
	i := slices.Index(s, domain.StepLoad)
	if i < 0 {
		return id
	}
	return p.intern(slices.Clone(s[:i+1]))
}

// Len is the number of interned paths.
func (p *Paths) Len() int { return len(p.steps) }

// String prints a path for test messages: ".1*.2" style.
func (p *Paths) String(id domain.PathID) string {
	var b strings.Builder
	for _, s := range p.steps[id] {
		switch s {
		case domain.StepLoad:
			b.WriteByte('*')
		case domain.StepElem:
			b.WriteString("[]")
		default:
			fmt.Fprintf(&b, ".%d", s)
		}
	}
	return b.String()
}

// Arena is a counting arena over Go slices.
type Arena struct {
	bytes, peak int
}

// Alloc returns n zeroed words.
func (a *Arena) Alloc(n int) []uint64 {
	a.bytes += 8 * n
	a.peak = max(a.peak, a.bytes)
	return make([]uint64, n)
}

// Bytes is what the arena holds.
func (a *Arena) Bytes() int { return a.bytes }

// Peak is the most it held.
func (a *Arena) Peak() int { return a.peak }

// Allocated is what Alloc handed out since Reset.
func (a *Arena) Allocated() int { return a.bytes }

// Reset frees everything.
func (a *Arena) Reset() { a.bytes = 0 }

// Release frees everything; the arena is not used after it.
func (a *Arena) Release() { a.bytes = 0 }

// Sets is a hash-consing set table.
type Sets struct {
	sets  [][]uint64
	index map[string]domain.SetID
}

// NewSets returns a table holding only the empty set.
func NewSets() *Sets {
	s := &Sets{}
	s.Reset()
	return s
}

func setKey(e []uint64) string {
	var b strings.Builder
	for _, x := range e {
		fmt.Fprintf(&b, "%x,", x)
	}
	return b.String()
}

// Intern returns the ID of the set of elems.
func (s *Sets) Intern(elems []uint64) domain.SetID {
	e := slices.Clone(elems)
	slices.Sort(e)
	e = slices.Compact(e)
	k := setKey(e)
	if id, ok := s.index[k]; ok {
		return id
	}
	id := domain.SetID(len(s.sets))
	s.sets = append(s.sets, e)
	s.index[k] = id
	return id
}

// Elems returns the elements of id in order.
func (s *Sets) Elems(id domain.SetID) []uint64 { return s.sets[id] }

// Union returns a ∪ b.
func (s *Sets) Union(a, b domain.SetID) domain.SetID {
	if a == b || b == domain.Empty {
		return a
	}
	if a == domain.Empty {
		return b
	}
	return s.Intern(append(slices.Clone(s.sets[a]), s.sets[b]...))
}

// Diff returns a \ b.
func (s *Sets) Diff(a, b domain.SetID) domain.SetID {
	if b == domain.Empty {
		return a
	}
	var out []uint64
	for _, x := range s.sets[a] {
		if !s.Has(b, x) {
			out = append(out, x)
		}
	}
	return s.Intern(out)
}

// Has reports whether x is in id.
func (s *Sets) Has(id domain.SetID, x uint64) bool {
	_, ok := slices.BinarySearch(s.sets[id], x)
	return ok
}

// Len is the size of id.
func (s *Sets) Len(id domain.SetID) int { return len(s.sets[id]) }

// Bytes estimates the memory held.
func (s *Sets) Bytes() int {
	n := 0
	for _, e := range s.sets {
		n += 8 * len(e)
	}
	return n
}

// Reset frees every set.
func (s *Sets) Reset() {
	s.sets = [][]uint64{nil}
	s.index = map[string]domain.SetID{"": domain.Empty}
}
