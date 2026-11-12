// Translated by go2pu from internal/escape2/domain/filter.go at the go-cutover tag; edited by hand since.

package domain

import (
	"compiler/compat"
	"compiler/internal/scratch"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/slices"
	"pluto/strings"
)

// Type filters on keys (decision D51; docs/plans/2026-10-24-type-filtered-summaries.md).
// A key may carry a filter: a type predicate and a position on its path (the
// number of steps before the filtered pointer). The key then stands only for
// the pointers found through a pointer, at that position, whose dynamic type
// the predicate admits: what a type assertion lets through, or the receiver
// of a method a call through an interface reaches. A filter never widens a
// key: the key without it covers it (Covers). A filter is a union of atoms:
// two entries that differ only in their filter are one entry with the union
// (summary.Widen), which keeps a join of many candidates as small as it is
// without filters.

// AtomKind is the kind of an atom of a type predicate.
type AtomKind uint8

const (
	// AtomImplements admits a dynamic type that implements interface T.
	AtomImplements AtomKind = iota + 1
	// AtomIs admits the dynamic type T alone.
	AtomIs
	// AtomPtrTo admits a pointer to an instance of generic type T (T is
	// the origin of a named type with type parameters, types2.Named.Origin):
	// a method of pointer receiver *G[P] is reached only by such a pointer.
	AtomPtrTo
	// AtomHolds admits a pointer to a value that holds a value of type T
	// in place (T itself, or a struct or array that holds one, at any
	// depth), or for T the origin of a generic named type an instance of
	// it: pluto/fmt's walk calls a method of pointer receiver *T on the
	// address of such a value's T (eir.Call.Fields).
	AtomHolds
)

// An Atom is one type predicate.
type Atom struct {
	Kind AtomKind
	T    types2.Type
}

// Admits reports whether the atom admits dynamic type d.
func (a Atom) Admits(d types2.Type) bool {
	switch a.Kind {
	case AtomImplements:
		if i, ok := a.T.Underlying().(*types2.Interface); ok {
			return types2.Implements(d, i)
		}
		return true
	case AtomIs:
		return types2.Identical(d, a.T)
	case AtomPtrTo:
		p, ok := types2.Unalias(d).(*types2.Pointer)
		return ok && SameOrigin(p.Elem(), a.T)
	}
	return true
}

func (a Atom) same(b Atom) bool { return a.Kind == b.Kind && types2.Identical(a.T, b.T) }

// Text is the atom's canonical text: its kind and its type, packages by
// path (the order of a union; the cache's encoding).
func (a Atom) Text() string {
	return compat.Concat(compat.RuneString(rune(rune('0'+a.Kind))), types2.TypeString(a.T, func(p *types2.Package) string { return p.Path() }))
}

// SameOrigin reports whether t is an instance (or the origin) of generic
// named type origin.
func SameOrigin(t, origin types2.Type) bool {
	n, ok := types2.Unalias(t).(*types2.Named)
	o, ok2 := types2.Unalias(origin).(*types2.Named)
	return ok && ok2 && n.Origin() == o.Origin()
}

// A Filter is a union of atoms, in a canonical order, and a conjunct
// further down the key's path: Inner, Rel steps past the filtered pointer
// (0: none). A key has room for one filter, and a join's member whose
// entry is filtered at a pointer of its own (its own call through an
// interface) and at its receiver keeps both so (Filters.Conj): the
// pointers it stands for pass the atoms at the filter's position, then
// the inner filter Rel steps further. The inner filter has no conjunct of
// its own (Conj). Admits and every other use of the
// atoms see the outer filter alone, which only widens.
type Filter struct {
	Atoms []Atom
	Inner FilterID
	Rel   uint8
}

// One is the filter of a single atom.
func One(kind AtomKind, T types2.Type) Filter { return Filter{Atoms: compat.Clone([]Atom{{kind, T}})} }

// Admits reports whether some atom admits dynamic type d.
func (f Filter) Admits(d types2.Type) bool {
	for _, a := range f.Atoms {
		if a.Admits(d) {
			return true
		}
	}
	return false
}

func (f Filter) same(g Filter) bool {
	return f.Inner == g.Inner && f.Rel == g.Rel && slices.EqualFunc(f.Atoms, g.Atoms, Atom.same)
}

// FilterID names an interned filter; 0 is none.
type FilterID uint16

// MaxFilters is the number of filters interned; past it, Intern gives none
// (0), which only widens the keys it would have filtered.
const MaxFilters = 1<<13 - 1

// MaxAtoms bounds the atoms of a union; a larger union is no filter.
const MaxAtoms = 1024

// Filters interns filters: one table per analysis, with its paths
// (Paths.Filters). It is safe for concurrent use: parallel solves make
// filters as they meet assertions and joins. Get takes no lock (the list
// only grows, and is published whole); Intern, Union and the covers test
// memoize. The IDs depend on the order filters are first made, which
// parallel solves do not fix: Canonical renumbers them once the analysis
// is over, so that its results do not depend on it.
type Filters struct {
	mu     compat.Mutex
	list   compat.AtomicPointer[[]Filter] // by ID-1
	byText map[string][]FilterID          // by the atoms' texts, in order
	keys   []string                       // each filter's atoms' texts, by ID-1
	text   map[Atom]string                // Atom.Text, memoized
	union  map[[2]FilterID]FilterID       // Union, memoized
	covers map[[2]FilterID]bool           // filterCovers, memoized

	// memo holds what the solvers derive from a filter's program (the
	// filter of an assertion's type, of a candidate's receiver), for the
	// analysis's lifetime (Memo).
	memo map[any]FilterID
}

// atomText is a.Text, memoized; fs.mu is held.
func (fs *Filters) atomText(a Atom) string {
	if t, ok := fs.text[a]; ok {
		return t
	}
	defer scratch.Outer().Restore()
	if fs.text == nil {
		fs.text = map[Atom]string{}
	}
	t := a.Text()
	fs.text[a] = t
	return t
}

// Intern returns the ID of f, adding it when no filter of the same atoms
// and conjunct is known; 0 when the table is full or f has no atom or too
// many. A conjunct Intern does not know is dropped.
func (fs *Filters) Intern(f Filter) FilterID {
	if fs == nil || len(f.Atoms) == 0 || len(f.Atoms) > MaxAtoms {
		return 0
	}
	for _, a := range f.Atoms {
		if a.T == nil {
			return 0
		}
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.intern(f)
}

// intern is Intern with fs.mu held. The filter's canonical form and its
// text are made in the current memory, a solve's scratch memory when a
// solve asks: only a filter not yet in the table is copied into the
// memory that outlives the solve (scratch.Outer). Most calls find their
// filter, and with the summary cache's sequential driver, which keeps
// nothing a solve allocates outside it, their copies took gigabytes.
func (fs *Filters) intern(f Filter) FilterID {
	// The canonical form: atoms sorted by their text, without duplicates.
	type at struct {
		a Atom
		t string
	}
	ats := compat.Make[at](len(f.Atoms))
	for i, a := range f.Atoms {
		ats[i] = at{a, fs.atomText(a)}
	}
	slices.SortStableFunc(ats, func(x, y at) int { return strings.Compare(x.t, y.t) })
	ats = slices.CompactFunc(ats, func(x, y at) bool { return x.t == y.t && x.a.same(y.a) })
	var key compat.Builder
	atoms := compat.Make[Atom](len(ats))
	for i, x := range ats {
		atoms[i] = x.a
		key.WriteString(x.t)
		key.WriteByte(0)
	}
	if f.Inner != 0 && f.Rel != 0 && int(f.Inner) <= len(fs.keys) {
		// The conjunct by its own text, so that the text names the
		// filter whatever the IDs (Canonical).
		key.WriteByte(1)
		key.WriteByte(f.Rel)
		key.WriteString(fs.keys[f.Inner-1])
	} else {
		f.Inner, f.Rel = 0, 0
	}
	f = Filter{Atoms: atoms, Inner: f.Inner, Rel: f.Rel}
	k := key.String()
	var old []Filter
	if l := fs.list.Load(); l != nil {
		old = *l
	}
	for _, id := range fs.byText[k] {
		if old[id-1].same(f) {
			return id
		}
	}
	if len(old) >= MaxFilters {
		return 0
	}
	defer scratch.Outer().Restore()
	f.Atoms = compat.Clone(f.Atoms)
	k = scratch.String(k)
	list := pu.Alloc[[]Filter]()
	(*list) = compat.Append(old, f)
	fs.list.Store(list)
	id := FilterID(len((*list)))
	if fs.byText == nil {
		fs.byText = map[string][]FilterID{}
	}
	fs.byText[k] = compat.AppendPlain(fs.byText[k], id)
	fs.keys = compat.Append(fs.keys, k)
	return id
}

// Get returns the filter of id; ok is false for 0 or an unknown id.
func (fs *Filters) Get(id FilterID) (Filter, bool) {
	if fs == nil || id == 0 {
		return Filter{}, false
	}
	l := fs.list.Load()
	if l == nil || int(id) > len(*l) {
		return Filter{}, false
	}
	return (*l)[id-1], true
}

// Union returns the ID of the union of filters a and b; 0 (no filter) when
// either is 0 or the union is too large. When both have a conjunct at the
// same distance, the union's conjunct is the union of theirs: (A and then
// X) or (B and then Y) is within (A or B) and then (X or Y), which keeps
// both narrowings of section 18.3 and widens only to the cross pairs.
// Otherwise it has none.
func (fs *Filters) Union(a, b FilterID) FilterID {
	if a == b {
		return a
	}
	if a > b {
		a, b = b, a
	}
	fa, ok1 := fs.Get(a)
	fb, ok2 := fs.Get(b)
	if !ok1 || !ok2 {
		return 0
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if u, ok := fs.union[[2]FilterID{a, b}]; ok {
		return u
	}
	var u FilterID
	if len(fa.Atoms)+len(fb.Atoms) <= 2*MaxAtoms {
		all := compat.Append(compat.Clone(fa.Atoms), fb.Atoms...)
		f := Filter{Atoms: all}
		if fa.Inner != 0 && fb.Inner != 0 && fa.Rel == fb.Rel {
			f.Inner, f.Rel = fs.unionInner(fa.Inner, fb.Inner), fa.Rel
		}
		u = fs.intern(f)
		if g, ok := fs.getLocked(u); ok && len(g.Atoms) > MaxAtoms {
			u = 0
		}
	}
	defer scratch.Outer().Restore()
	if fs.union == nil {
		fs.union = map[[2]FilterID]FilterID{}
	}
	fs.union[[2]FilterID{a, b}] = u
	return u
}

// UnionAll returns the ID of the union of every filter of ids, as Union
// would fold them, interned once: 0 (no filter) when one is 0 or the union
// is too large. Folding with Union interned each partial union, one filter
// per step: the joins of the compiler's self-build, a hundred receivers'
// filters each and their conjuncts, filled the table (MaxFilters), and
// every later filter, Intern's 0, became no filter: what any caller passed
// to fmt.Errorf was then taken for kept, through the join of every String
// method.
func (fs *Filters) UnionAll(ids []FilterID) FilterID {
	if len(ids) == 0 {
		return 0
	}
	same := true
	for _, id := range ids {
		if id == 0 {
			return 0
		}
		same = same && id == ids[0]
	}
	if same {
		return ids[0]
	}
	u := compat.Clone(ids)
	slices.Sort(u)
	u = slices.Compact(u)
	var atoms, inner []Atom
	keepInner, rel := true, uint8(0)
	for i, id := range u {
		f, ok := fs.Get(id)
		if !ok {
			return 0
		}
		atoms = compat.Append(atoms, f.Atoms...)
		if keepInner {
			g, ok := fs.Get(f.Inner)
			if !ok || i > 0 && f.Rel != rel {
				keepInner = false
			} else {
				rel = f.Rel
				inner = compat.Append(inner, g.Atoms...)
			}
		}
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.tooMany(atoms) {
		return 0
	}
	f := Filter{Atoms: atoms}
	if keepInner && !fs.tooMany(inner) {
		if in := fs.intern(Filter{Atoms: inner}); in != 0 {
			if g, ok := fs.getLocked(in); ok && len(g.Atoms) <= MaxAtoms {
				f.Inner, f.Rel = in, rel
			}
		}
	}
	id := fs.intern(f)
	if g, ok := fs.getLocked(id); !ok || len(g.Atoms) > MaxAtoms {
		return 0
	}
	return id
}

// tooMany reports whether atoms has more than MaxAtoms distinct texts,
// under fs.mu: a union that large is no filter, and is not interned.
func (fs *Filters) tooMany(atoms []Atom) bool {
	if len(atoms) <= MaxAtoms {
		return false
	}
	seen := map[string]bool{}
	for _, a := range atoms {
		seen[fs.atomText(a)] = true
	}
	return len(seen) > MaxAtoms
}

// unionInner is the union of conjuncts a and b (which have none of their
// own), under fs.mu: 0 (none) when it is too large.
func (fs *Filters) unionInner(a, b FilterID) FilterID {
	if a == b {
		return a
	}
	fa, ok1 := fs.getLocked(a)
	fb, ok2 := fs.getLocked(b)
	if !ok1 || !ok2 || len(fa.Atoms)+len(fb.Atoms) > 2*MaxAtoms {
		return 0
	}
	u := fs.intern(Filter{Atoms: compat.Append(compat.Clone(fa.Atoms), fb.Atoms...)})
	if g, ok := fs.getLocked(u); !ok || len(g.Atoms) > MaxAtoms {
		return 0
	}
	return u
}

// Conj returns the ID of filter outer with conjunct inner rel steps past
// the pointer outer filters (Filter.Inner): outer when inner is 0, rel is
// out of range or outer has a conjunct already, which only widens. A
// conjunct has no conjunct of its own: inner's is dropped, which only
// widens too. A chain of them, one per call through an interface on the
// way (a method of types2's Type calling Underlying on a field of its
// receiver, recursively), made a distinct filter per chain, and the
// tokens of (*operand).convertibleTo went past MaxFuncArena (E3010).
func (fs *Filters) Conj(outer, inner FilterID, rel int) FilterID {
	fo, ok := fs.Get(outer)
	if !ok || inner == 0 || rel <= 0 || rel > maxFilterPos || fo.Inner != 0 {
		return outer
	}
	fi, ok := fs.Get(inner)
	if !ok {
		return outer
	}
	defer scratch.Outer().Restore()
	return fs.Memo(pu.Box(conjKey{outer, inner, rel}), func() FilterID {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		in := inner
		if fi.Inner != 0 {
			if in = fs.intern(Filter{Atoms: fi.Atoms}); in == 0 {
				return outer
			}
		}
		if c := fs.intern(Filter{Atoms: fo.Atoms, Inner: in, Rel: uint8(rel)}); c != 0 {
			return c
		}
		return outer
	})
}

type conjKey struct {
	outer, inner FilterID
	rel          int
}

// InnerOf returns the conjunct of filter id and its distance (0: none).
func (fs *Filters) InnerOf(id FilterID) (FilterID, int) {
	f, _ := fs.Get(id)
	return f.Inner, int(f.Rel)
}

// Outer returns the ID of filter id without its conjunct, which only
// widens: id itself when it has none; 0 (no filter) when the table is full.
func (fs *Filters) Outer(id FilterID) FilterID {
	f, ok := fs.Get(id)
	if !ok || f.Inner == 0 {
		return id
	}
	defer scratch.Outer().Restore()
	return fs.Memo(pu.Box(outerKey{id}), func() FilterID {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return fs.intern(Filter{Atoms: f.Atoms})
	})
}

type outerKey struct{ id FilterID }

func (fs *Filters) getLocked(id FilterID) (Filter, bool) {
	l := fs.list.Load()
	if id == 0 || l == nil || int(id) > len(*l) {
		return Filter{}, false
	}
	return (*l)[id-1], true
}

// coversID reports whether filter a's atoms include filter b's, and a's
// conjunct, if any, covers b's at the same distance, memoized.
func (fs *Filters) coversID(a, b FilterID) bool {
	fa, ok1 := fs.Get(a)
	fb, ok2 := fs.Get(b)
	if !ok1 || !ok2 {
		return false
	}
	fs.mu.Lock()
	c, ok := fs.covers[[2]FilterID{a, b}]
	fs.mu.Unlock()
	if ok {
		return c
	}
	defer scratch.Outer().Restore()
	c = fa.Inner == 0 || fb.Inner != 0 && fa.Rel == fb.Rel && (fa.Inner == fb.Inner || fs.coversID(fa.Inner, fb.Inner))
	for _, x := range fb.Atoms {
		if !c {
			break
		}
		if !slices.ContainsFunc(fa.Atoms, x.same) {
			c = false
			break
		}
	}
	fs.mu.Lock()
	if fs.covers == nil {
		fs.covers = map[[2]FilterID]bool{}
	}
	fs.covers[[2]FilterID{a, b}] = c
	fs.mu.Unlock()
	return c
}

// maxFilterPos is the last position a filter may have (three bits of the
// key); a filter past it is dropped, which only widens the key.
const maxFilterPos = 7

const (
	keyFilterShift = 46
	keyFilterMask  = Key(MaxFilters) << keyFilterShift
	keyPosShift    = 60
	keyPosMask     = Key(7) << keyPosShift
)

// Filter returns the key's filter and its position, or 0.
func (k Key) Filter() (FilterID, int) {
	return FilterID((k & keyFilterMask) >> keyFilterShift), int((k & keyPosMask) >> keyPosShift)
}

// WithFilter returns k with filter id at position pos (the number of path
// steps before the filtered pointer); id 0, or a position past the key's
// path or past maxFilterPos, gives k without a filter.
func (k Key) WithFilter(ps Paths, id FilterID, pos int) Key {
	k = k.NoFilter()
	if id == 0 || k.Arena() || pos < 0 || pos > maxFilterPos || pos > len(ps.Steps(k.Path())) {
		return k
	}
	return k | Key(id)<<keyFilterShift | Key(pos)<<keyPosShift
}

// WithFilterRaw is WithFilter for a position known to lie within the key's
// path (a solver's token keeps it so).
func (k Key) WithFilterRaw(id FilterID, pos int) Key {
	k = k.NoFilter()
	if id == 0 || k.Arena() || pos < 0 || pos > maxFilterPos {
		return k
	}
	return k | Key(id)<<keyFilterShift | Key(pos)<<keyPosShift
}

// NoFilter returns k without its filter.
func (k Key) NoFilter() Key { return k &^ (keyFilterMask | keyPosMask) }

// HasFilter reports whether k has a filter.
func (k Key) HasFilter() bool { return k&keyFilterMask != 0 }

// filterCovers reports whether a's filter covers b's: both at one position,
// every atom of b's an atom of a's.
func filterCovers(fs *Filters, a, b Key) bool {
	ia, pa := a.Filter()
	ib, pb := b.Filter()
	if ib == 0 || pa != pb {
		return false
	}
	if ia == ib {
		return true
	}
	return fs.coversID(ia, ib)
}

// Canonical renumbers the filters by their atoms' texts (filters of the
// same texts, of distinct local types, keep their order) and returns the
// renaming, by old ID (nil: none changes). The keys that name the old IDs
// must be renamed with it (Key.WithFilterRaw); the memos are dropped. It
// must not run beside any other use of the table.
func (fs *Filters) Canonical() []FilterID {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	l := fs.list.Load()
	if l == nil {
		return nil
	}
	list := *l
	order := compat.Make[int](len(list))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return strings.Compare(fs.keys[a], fs.keys[b]) })
	rename := compat.Make[FilterID](len(list) + 1)
	same := true
	newList := pu.New(compat.Make[Filter](len(list)))
	newKeys := compat.Make[string](len(list))
	fs.byText = map[string][]FilterID{}
	for n, old := range order {
		id := FilterID(n + 1)
		rename[old+1] = id
		same = same && old == n
		(*newList)[n], newKeys[n] = list[old], fs.keys[old]
		fs.byText[newKeys[n]] = compat.AppendPlain(fs.byText[newKeys[n]], id)
	}
	for n := range *newList {
		if in := (*newList)[n].Inner; in != 0 {
			(*newList)[n].Inner = rename[in]
		}
	}
	fs.list.Store(newList)
	fs.keys = newKeys
	fs.union, fs.covers = nil, nil
	fs.memo = nil
	if same {
		return nil
	}
	return rename
}

// Memo returns the filter memoized for k, computing it with compute (which
// interns it in fs) the first time: the filter of an assertion's type or
// of a candidate's receiver, which the solvers ask for at every solve.
func (fs *Filters) Memo(k any, compute func() FilterID) FilterID {
	defer scratch.Outer().Restore()
	fs.mu.Lock()
	id, ok := fs.memo[k]
	fs.mu.Unlock()
	if ok {
		return id
	}
	id = compute()
	fs.mu.Lock()
	if fs.memo == nil {
		fs.memo = map[any]FilterID{}
	}
	fs.memo[k] = id
	fs.mu.Unlock()
	return id
}
