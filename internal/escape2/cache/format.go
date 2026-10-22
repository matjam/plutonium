package cache

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// The encoding of a summary, canonical: the same summary gives the same
// bytes in every run, whatever the run's path IDs. A path is written as its
// steps, a key as its input, flags and path, and each set is sorted by its
// encoded bytes. Reasons are not written: they explain findings, and no
// finding is reported from a summary that depends on the file (sched,
// redoSCCs), so every reason a message reads comes from a solve of this
// run.
//
//	summary  := byte(0) (bottom, nil)
//	          | byte(1) uvarint(inputs) keys(esc) pairs(store) keys(heapstore)
//	            uvarint(len res) result* byte(region builds) uvarint(n) uvarint(outer)*
//	            uvarint(n) deferred*  (sorted by bytes)
//	deferred := string(origin stable ID) uvarint(site) key(fn) uvarint(n) (byte(heap) keys)*
//	keys     := uvarint(n) key*            (sorted by bytes)
//	pairs    := uvarint(n) (key key)*      (sorted by bytes)
//	key      := uvarint(input) byte(flags: 1 GE, 2 Borrowed, 4 Counts, 8 DirectCount, 16 FieldsGE) path
//	path     := uvarint(n) varint(step)*   (StepLoad -2, StepElem -1, field >= 0)
//	result   := uvarint(n) (key path byte(fieldGE))* paths(heap) paths(counted)
//	paths    := uvarint(n) path*           (sorted by bytes)

const (
	flagGE          = 1
	flagBorrowed    = 2
	flagCounts      = 4
	flagDirectCount = 8
	flagFieldsGE    = 16
)

func appendPath(b []byte, ps domain.Paths, p domain.PathID) []byte {
	steps := ps.Steps(p)
	b = binary.AppendUvarint(b, uint64(len(steps)))
	for _, s := range steps {
		b = binary.AppendVarint(b, int64(s))
	}
	return b
}

func appendKey(b []byte, ps domain.Paths, k domain.Key) []byte {
	b = binary.AppendUvarint(b, uint64(k.Input()))
	f := k.Flags()
	var fl byte
	if f.GE {
		fl |= flagGE
	}
	if f.Borrowed {
		fl |= flagBorrowed
	}
	if f.Counts {
		fl |= flagCounts
	}
	if f.DirectCount {
		fl |= flagDirectCount
	}
	if f.FieldsGE {
		fl |= flagFieldsGE
	}
	b = append(b, fl)
	return appendPath(b, ps, k.Path())
}

// appendSorted appends n encoded items, sorted by their bytes.
func appendSorted(b []byte, items [][]byte) []byte {
	slices.SortFunc(items, bytes.Compare)
	b = binary.AppendUvarint(b, uint64(len(items)))
	for _, it := range items {
		b = append(b, it...)
	}
	return b
}

func appendKeys(b []byte, ps domain.Paths, keys []domain.Key) []byte {
	items := make([][]byte, len(keys))
	for i, k := range keys {
		items[i] = appendKey(nil, ps, k)
	}
	return appendSorted(b, items)
}

func appendPaths(b []byte, ps domain.Paths, paths []domain.PathID) []byte {
	items := make([][]byte, len(paths))
	for i, p := range paths {
		items[i] = appendPath(nil, ps, p)
	}
	return appendSorted(b, items)
}

// appendSummary appends the canonical encoding of s.
// name gives the stable ID of a deferred call's origin; ok is false when an
// origin has none (the summary cannot be written then).
func appendSummary(b []byte, ps domain.Paths, s *summary.Summary, name func(eir.FuncID) string) (_ []byte, ok bool) {
	if s == nil {
		return append(b, 0), true
	}
	b = append(b, 1)
	b = binary.AppendUvarint(b, uint64(s.Inputs))
	b = appendKeys(b, ps, s.Esc)
	items := make([][]byte, len(s.Store))
	for i, p := range s.Store {
		items[i] = appendKey(appendKey(nil, ps, p.Src), ps, p.Dst)
	}
	b = appendSorted(b, items)
	b = appendKeys(b, ps, s.HeapStore)
	b = binary.AppendUvarint(b, uint64(len(s.Res)))
	for _, r := range s.Res {
		items := make([][]byte, len(r.From))
		for i, rk := range r.From {
			it := appendPath(appendKey(nil, ps, rk.Key), ps, rk.Field)
			if rk.FieldGE {
				it = append(it, 1)
			} else {
				it = append(it, 0)
			}
			items[i] = it
		}
		b = appendSorted(b, items)
		b = appendPaths(b, ps, r.Heap)
		b = appendPaths(b, ps, r.CountedHeap)
	}
	if s.Region.Builds {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	outer := slices.Clone(s.Region.OuterResults)
	slices.Sort(outer)
	outer = slices.Compact(outer)
	b = binary.AppendUvarint(b, uint64(len(outer)))
	for _, r := range outer {
		b = binary.AppendUvarint(b, uint64(r))
	}
	ok = true
	items = make([][]byte, len(s.Deferred))
	for i, d := range s.Deferred {
		n := name(d.Origin)
		if n == "" {
			ok = false
		}
		it := appendString(nil, n)
		it = binary.AppendUvarint(it, uint64(d.Site))
		it = appendKey(it, ps, d.Fn)
		it = binary.AppendUvarint(it, uint64(len(d.Args)))
		for _, a := range d.Args {
			if a.Heap {
				it = append(it, 1)
			} else {
				it = append(it, 0)
			}
			it = appendKeys(it, ps, a.Keys)
		}
		items[i] = it
	}
	return appendSorted(b, items), ok
}

// errCorrupt is what decoding reports for bytes no encoder writes.
var errCorrupt = errors.New("corrupt")

// reader decodes, failing on the first error: every count is checked
// against the bytes left, so a corrupt file cannot make it allocate much.
type reader struct {
	b   []byte
	err error
}

func (r *reader) fail() {
	if r.err == nil {
		r.err = errCorrupt
	}
}

func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *reader) varint() int64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Varint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

// count reads a count of items of at least min bytes each.
func (r *reader) count(min int) int {
	n := r.uvarint()
	if r.err == nil && n > uint64(len(r.b)/min) {
		r.fail()
		return 0
	}
	return int(n)
}

func (r *reader) byte1() byte {
	if r.err != nil {
		return 0
	}
	if len(r.b) == 0 {
		r.fail()
		return 0
	}
	c := r.b[0]
	r.b = r.b[1:]
	return c
}

func (r *reader) bytesN(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.b) {
		r.fail()
		return nil
	}
	out := r.b[:n:n]
	r.b = r.b[n:]
	return out
}

func (r *reader) str() string { return string(r.bytesN(r.count(1))) }

// path interns a path read from the file. A path the interner would cut
// (past the limits of section 18.3) is not one an encoder writes: corrupt.
func (r *reader) path(ps domain.Paths) domain.PathID {
	n := r.count(1)
	if n > domain.MaxSteps {
		r.fail()
	}
	p := domain.Root
	for i := 0; i < n && r.err == nil; i++ {
		s := r.varint()
		if s < int64(domain.StepLoad) || s > 1<<20 {
			r.fail()
			break
		}
		q, cut := ps.Extend(p, domain.Step(s))
		if cut || q == p {
			r.fail()
			break
		}
		p = q
	}
	return p
}

func (r *reader) key(ps domain.Paths, inputs int) domain.Key {
	in := r.uvarint()
	fl := r.byte1()
	p := r.path(ps)
	if r.err != nil {
		return 0
	}
	if in >= uint64(inputs) || in >= domain.MaxInputs || fl&^0x1f != 0 {
		r.fail()
		return 0
	}
	return domain.MakeKey(int(in), p, domain.KeyFlags{
		GE: fl&flagGE != 0, Borrowed: fl&flagBorrowed != 0, Counts: fl&flagCounts != 0, DirectCount: fl&flagDirectCount != 0, FieldsGE: fl&flagFieldsGE != 0,
	})
}

// minKey is the fewest bytes a key takes.
const minKey = 3

// shape is what a summary must agree with of its function, so that a
// caller applying it cannot index past its inputs or results.
type shape struct {
	inputs, results int
}

// decodeSummary decodes a summary encoded by appendSummary, interning its
// paths in ps, and checks it against the function's shape. Its sets are
// sorted as the solver keeps them (by key); nothing else about them is
// assumed: the driver's check decides whether the summary stands.
func decodeSummary(b []byte, ps domain.Paths, sh shape, id func(string) (eir.FuncID, bool)) (*summary.Summary, error) {
	r := &reader{b: b}
	s := r.summary(ps, sh, id)
	if r.err == nil && len(r.b) != 0 {
		r.fail()
	}
	if r.err != nil {
		return nil, r.err
	}
	return s, nil
}

func (r *reader) summary(ps domain.Paths, sh shape, id func(string) (eir.FuncID, bool)) *summary.Summary {
	switch r.byte1() {
	case 0:
		return nil
	case 1:
	default:
		r.fail()
		return nil
	}
	s := &summary.Summary{Inputs: int(r.uvarint())}
	if r.err == nil && s.Inputs != sh.inputs {
		r.fail()
	}
	keys := func() []domain.Key {
		n := r.count(minKey)
		var out []domain.Key
		for i := 0; i < n && r.err == nil; i++ {
			out = append(out, r.key(ps, s.Inputs))
		}
		slices.Sort(out)
		return out
	}
	paths := func() []domain.PathID {
		n := r.count(1)
		var out []domain.PathID
		for i := 0; i < n && r.err == nil; i++ {
			out = append(out, r.path(ps))
		}
		slices.Sort(out)
		return out
	}
	s.Esc = keys()
	n := r.count(2 * minKey)
	for i := 0; i < n && r.err == nil; i++ {
		s.Store = append(s.Store, summary.StorePair{Src: r.key(ps, s.Inputs), Dst: r.key(ps, s.Inputs)})
	}
	slices.SortFunc(s.Store, func(a, b summary.StorePair) int {
		if a.Src != b.Src {
			return cmpKey(a.Src, b.Src)
		}
		return cmpKey(a.Dst, b.Dst)
	})
	s.HeapStore = keys()
	nres := r.count(3)
	if r.err == nil && nres > sh.results {
		r.fail()
	}
	for i := 0; i < nres && r.err == nil; i++ {
		var res summary.Result
		n := r.count(minKey + 2)
		for j := 0; j < n && r.err == nil; j++ {
			rk := summary.ResultKey{Key: r.key(ps, s.Inputs), Field: r.path(ps)}
			switch r.byte1() {
			case 0:
			case 1:
				rk.FieldGE = true
			default:
				r.fail()
			}
			res.From = append(res.From, rk)
		}
		slices.SortFunc(res.From, func(a, b summary.ResultKey) int {
			if a.Key != b.Key {
				return cmpKey(a.Key, b.Key)
			}
			if a.Field != b.Field {
				return int(a.Field) - int(b.Field)
			}
			return boolInt(a.FieldGE) - boolInt(b.FieldGE)
		})
		res.Heap = paths()
		res.CountedHeap = paths()
		s.Res = append(s.Res, res)
	}
	switch r.byte1() {
	case 0:
	case 1:
		s.Region.Builds = true
	default:
		r.fail()
	}
	n = r.count(1)
	for i := 0; i < n && r.err == nil; i++ {
		o := r.uvarint()
		if o >= uint64(sh.results) {
			r.fail()
			break
		}
		s.Region.OuterResults = append(s.Region.OuterResults, int(o))
	}
	nd := r.count(4)
	for i := 0; i < nd && r.err == nil; i++ {
		var d summary.Deferred
		origin, ok := id(r.str())
		if !ok {
			r.fail()
			break
		}
		d.Origin = origin
		d.Site = eir.SiteID(r.uvarint())
		d.Fn = r.key(ps, s.Inputs)
		na := r.count(2)
		for j := 0; j < na && r.err == nil; j++ {
			var a summary.DeferredArg
			switch r.byte1() {
			case 0:
			case 1:
				a.Heap = true
			default:
				r.fail()
			}
			a.Keys = keys()
			d.Args = append(d.Args, a)
		}
		s.Deferred = append(s.Deferred, d)
	}
	if r.err != nil {
		return nil
	}
	return s
}

func cmpKey(a, b domain.Key) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s shape) String() string { return fmt.Sprintf("%d/%d", s.inputs, s.results) }
