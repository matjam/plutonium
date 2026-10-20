package escape

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/types2"
)

// The tests check that every program the tests analyse takes the fast way of
// following a key from a parameter pointer (fastKey) and that way gives what
// the general one does.
func init() { checkFast = true }

// The functions below are the analysis as it was before it followed a path
// one pointer at a time and built sets with an index: a set at a time, and a
// search of the set for every token added. walkKey must give the same sets in
// the same order.

func refAdd(s *tset, t tok) {
	for _, u := range *s {
		if u == t {
			return
		}
	}
	*s = append(*s, t)
}

func refLoad(ts tset) tset {
	var out tset
	for _, t := range ts {
		switch t.kind {
		case tHeap:
			refAdd(&out, heapTok)
		case tFrame:
			if t.loc.kind == lkObject || t.loc.kind == lkArena || t.loc.object {
				refAdd(&out, heapTok)
				continue
			}
			for _, u := range t.loc.get(t.path) {
				refAdd(&out, u)
			}
		case tParam:
			refAdd(&out, t.untag().extend("*"))
		}
	}
	return out
}

func refOffset(ts tset, i int) tset {
	var out tset
	for _, t := range ts {
		t = t.untag()
		if t.kind != tHeap && fieldTail(t.path) >= maxFieldNest {
			refAdd(&out, t)
			continue
		}
		switch t.kind {
		case tFrame:
			t.path += fieldStep(i)
		case tParam:
			t = t.extend(fieldStep(i))
		}
		refAdd(&out, t)
	}
	return out
}

func refFilter(ts tset, I types2.Type) tset {
	iface, _ := I.Underlying().(*types2.Interface)
	exact := iface == nil
	if exact && containsTypeParam(I) {
		return ts
	}
	if !exact && iface.Empty() {
		return ts
	}
	var out tset
	for _, t := range ts {
		switch t.kind {
		case tFrame:
			if dyn := t.loc.dynamicType(); t.path == "" && dyn != nil && !containsTypeParam(dyn) {
				if exact && !types2.Identical(dyn, I) && !types2.Identical(dyn, types2.NewPointer(I)) {
					continue
				}
				if !exact && !types2.Implements(dyn, iface) {
					continue
				}
			}
		case tParam:
			if t.filt == nil && !t.ge {
				t.filt, t.filtAt = I, len(t.path)
			}
		}
		refAdd(&out, t)
	}
	return out
}

func refReach(ts tset) tset {
	widen := func(ts tset) tset {
		var out tset
		for _, t := range ts {
			t = t.untag()
			if t.kind == tParam {
				t = t.widen()
			}
			refAdd(&out, t)
		}
		return out
	}
	out := widen(ts)
	for {
		grew := false
		for _, t := range widen(refLoad(out)) {
			if !out.has(t) {
				out = append(out, t)
				grew = true
			}
		}
		if !grew {
			return out
		}
	}
}

func refWalkKey(ts tset, k pkey) tset {
	cur := ts
	at := 0
	for _, step := range splitPath(k.path) {
		if k.filt != nil && at == k.filtAt {
			cur = refFilter(cur, k.filt)
		}
		at += len(step)
		if step == "*" {
			cur = refLoad(cur)
			continue
		}
		i, _ := strconv.Atoi(strings.TrimSuffix(step, "."))
		cur = refOffset(cur, i)
	}
	if k.filt != nil && at == k.filtAt {
		cur = refFilter(cur, k.filt)
	}
	if !k.ge {
		return cur
	}
	return refReach(cur)
}

// randomWorld makes frame locations that hold one another, parameter scopes
// and types for filters.
type randomWorld struct {
	rng    *rand.Rand
	locs   []*loc
	scopes []*scope
	filts  []types2.Type
}

func newRandomWorld(seed int64) *randomWorld {
	w := &randomWorld{rng: rand.New(rand.NewSource(seed))}
	fn := &function{name: "f"}
	w.scopes = []*scope{{fn: fn}, {fn: &function{name: "g"}, level: 1}}
	w.filts = []types2.Type{
		types2.Universe.Lookup("error").Type(), // an interface
		types2.Universe.Lookup("any").Type(),   // the empty interface, which keeps everything
		types2.Typ[types2.String],              // a concrete type
	}
	kinds := []locKind{lkVar, lkVar, lkBox, lkObject, lkArena, lkVar, lkTemp}
	types := []types2.Type{nil, types2.Typ[types2.Int], types2.Typ[types2.String], types2.NewPointer(types2.Typ[types2.Int])}
	for i := 0; i < 8; i++ {
		l := &loc{kind: kinds[w.rng.Intn(len(kinds))], typ: types[w.rng.Intn(len(types))], box: w.rng.Intn(3) == 0, object: w.rng.Intn(9) == 0}
		w.locs = append(w.locs, l)
	}
	for _, l := range w.locs {
		for n := w.rng.Intn(6); n > 0; n-- {
			l.put(w.randomPath(2, false), tset{w.randomTok(false)})
		}
	}
	return w
}

func (w *randomWorld) randomPath(maxSteps int, loads bool) string {
	var b strings.Builder
	for n := w.rng.Intn(maxSteps + 1); n > 0; n-- {
		if loads && w.rng.Intn(2) == 0 {
			b.WriteString("*")
		} else {
			fmt.Fprintf(&b, "%d.", w.rng.Intn(13))
		}
	}
	return b.String()
}

func (w *randomWorld) randomTok(param bool) tok {
	r := w.rng.Intn(10)
	switch {
	case param || r < 4:
		t := paramTok(w.scopes[w.rng.Intn(len(w.scopes))], w.rng.Intn(3))
		for _, step := range splitPath(w.randomPath(5, true)) {
			t = t.extend(step)
		}
		t.borrowed, t.counted = w.rng.Intn(4) == 0, w.rng.Intn(5) == 0
		if w.rng.Intn(3) == 0 {
			t.filt, t.filtAt = w.filts[w.rng.Intn(len(w.filts))], len(t.path)
		}
		if w.rng.Intn(3) == 0 {
			t.tag = fieldStep(w.rng.Intn(3))
		}
		return t
	case r < 8:
		t := frameTok(w.locs[w.rng.Intn(len(w.locs))])
		t.path = w.randomPath(2, false)
		if w.rng.Intn(4) == 0 {
			t.tag = fieldStep(w.rng.Intn(3))
		}
		return t
	}
	t := heapTok
	t.counted = w.rng.Intn(3) == 0
	return t
}

func (w *randomWorld) randomKey() pkey {
	k := pkey{idx: w.rng.Intn(3), path: w.randomPath(6, true), ge: w.rng.Intn(3) == 0, borrowed: w.rng.Intn(3) == 0}
	if w.rng.Intn(2) == 0 {
		k.filt = w.filts[w.rng.Intn(len(w.filts))]
		k.filtAt = w.rng.Intn(len(k.path) + 3)
		// Filters apply where a step begins; others never apply.
		if w.rng.Intn(2) == 0 {
			k.filtAt = 0
			for _, s := range splitPath(k.path)[:w.rng.Intn(len(splitPath(k.path))+1)] {
				k.filtAt += len(s)
			}
		}
	}
	return k
}

func sameSet(a, b tset) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestWalkKeyMatchesSetAtATime compares walkKey with following the path a
// set at a time, over random pointers, locations and keys.
func TestWalkKeyMatchesSetAtATime(t *testing.T) {
	for seed := int64(0); seed < 300; seed++ {
		w := newRandomWorld(seed)
		wk := &walker{a: &analyzer{}}
		for n := 0; n < 200; n++ {
			var ts tset
			for i := w.rng.Intn(5); i > 0; i-- {
				ts = append(ts, w.randomTok(false))
			}
			// A set has no repeats.
			var set tbuf
			for _, tk := range ts {
				set.add(tk)
			}
			ts = set.set
			k := w.randomKey()
			want := refWalkKey(ts, k)
			got := wk.walkKey(ts, k)
			if !sameSet(want, got) {
				t.Fatalf("seed %d: walkKey(%v, %+v)\n got %v\nwant %v", seed, ts, k, got, want)
			}
		}
	}
}

// TestDeriveKeyMatchesGeneralWay compares the key fastKey gives for a
// parameter pointer with the one of the pointer walkKey finds.
func TestDeriveKeyMatchesGeneralWay(t *testing.T) {
	for seed := int64(0); seed < 300; seed++ {
		w := newRandomWorld(seed)
		wk := &walker{a: &analyzer{}}
		for n := 0; n < 300; n++ {
			tk := w.randomTok(true)
			k := w.randomKey()
			k.counts = false
			f := &function{}
			var buf [64]byte
			key, ok, once := wk.fastKey(f, 1, recvExact, false, tk, k, &buf)
			if !ok || once {
				t.Fatalf("seed %d: fastKey(%+v, %+v) = ok %v, once %v", seed, tk, k, ok, once)
			}
			// The general way: borrow, then follow the path.
			actual := tset{tk}
			if k.borrowed {
				actual = borrowHolders(actual)
			}
			got := wk.walkParam(f, actual, k)
			if len(got) != 1 || got[0].kind != tParam || keyOf(got[0]) != key {
				t.Fatalf("seed %d: fastKey(%+v, %+v) = %+v; the general way gives %v", seed, tk, k, key, got)
			}
		}
	}
}

// TestTbufKeepsOrder checks that a tbuf holds the tokens in the order they
// were first added, as a tset does, whatever the size.
func TestTbufKeepsOrder(t *testing.T) {
	w := newRandomWorld(7)
	for n := 0; n < 200; n++ {
		var want tset
		var got tbuf
		for i := w.rng.Intn(60); i > 0; i-- {
			tk := w.randomTok(w.rng.Intn(2) == 0)
			if w.rng.Intn(3) == 0 && len(want) > 0 {
				tk = want[w.rng.Intn(len(want))] // a repeat
			}
			a, b := want.add(tk), got.add(tk)
			if a != b {
				t.Fatalf("add(%v) = %v, want %v", tk, b, a)
			}
		}
		if !sameSet(want, got.set) {
			t.Fatalf("got %v, want %v", got.set, want)
		}
	}
}

// TestEscapeSet checks the table of the escapes of a summary against a map,
// through growth and keys that differ in one field.
func TestEscapeSet(t *testing.T) {
	var s summary
	want := map[pkey]bool{}
	w := newRandomWorld(11)
	for n := 0; n < 20000; n++ {
		k := w.randomKey()
		k.counts = w.rng.Intn(2) == 0
		if s.hasEscape(k) != want[k] {
			t.Fatalf("hasEscape(%+v) = %v before adding", k, s.hasEscape(k))
		}
		added := s.addEscape(k, reason{text: "r"})
		if added == want[k] {
			t.Fatalf("addEscape(%+v) = %v, want %v", k, added, !want[k])
		}
		want[k] = true
		if r := s.escapeReason(k); r == nil || r.String() != "r" || r.ent == nil || r.ent.k != k {
			t.Fatalf("reason of %+v is %+v", k, r)
		}
	}
	if s.numEscapes() != len(want) {
		t.Fatalf("%d escapes, want %d", s.numEscapes(), len(want))
	}
	seen := 0
	for k := range s.escapeKeys() {
		if !want[k] {
			t.Fatalf("key %+v is not one added", k)
		}
		seen++
	}
	if seen != len(want) {
		t.Fatalf("%d keys listed, want %d", seen, len(want))
	}
}
