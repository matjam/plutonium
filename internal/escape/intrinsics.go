package escape

import "fmt"

// Library functions the compiler expands inline (internal/lower, strops.go)
// have bodies that never run: a placeholder that panics, or a call of the
// function itself. Their summaries cannot come from those bodies, which say
// that they keep, store and return nothing. They come from this table, by
// name, as the runtime's other contracts with the compiler do: what the
// expansion does with the pointers it is passed. They are callable from safe
// code (language spec section 18.12), and the containers of the standard
// library are built on them, so a summary that said less than the expansion
// does would let a container keep a view of a frame (section 18.3).
//
// A pointer that only selects memory (p, the address of an element) is the
// key {idx}; the pointers stored in what it points to are {idx, "*"}.
type intrinsicEffect struct {
	// stores are the stores the expansion makes: src's pointers into the
	// memory dst points to.
	stores []intrinsicStore
	// results are, for each result, the keys its pointers come from.
	results [][]pkey
}

type intrinsicStore struct {
	src, dst pkey
	what     string // for messages: what is stored where
}

func argSelf(i int) pkey { return pkey{idx: i} }
func argLoad(i int) pkey { return pkey{idx: i, path: "*"} }

var intrinsicEffects = map[string]intrinsicEffect{
	// InitValue(p, x) moves x into *p.
	"pluto/rt.InitValue": {stores: []intrinsicStore{{argSelf(1), argSelf(0), "x in *p"}}},
	// MoveValues and CopyValues(dst, src, n) copy the values at src to dst.
	"pluto/rt.MoveValues": {stores: []intrinsicStore{{argLoad(1), argSelf(0), "the values at src in *dst"}}},
	"pluto/rt.CopyValues": {stores: []intrinsicStore{{argLoad(1), argSelf(0), "the values at src in *dst"}}},
	// CopyValue(p) returns *p.
	"pluto/rt.CopyValue": {results: [][]pkey{{argLoad(0)}}},
	// ViewOf(p) returns *p, or p itself for an owner.
	"pluto/rt.ViewOf": {results: [][]pkey{{argSelf(0), argLoad(0)}}},
	// SliceBytewise(s, lo, hi), strings.Slice(s, i, j) and
	// strings.SliceFrom(s, i) return a substring of s, which shares its
	// buffer.
	"pluto/rt.SliceBytewise":  {results: [][]pkey{{argSelf(0)}}},
	"pluto/strings.Slice":     {results: [][]pkey{{argSelf(0)}}},
	"pluto/strings.SliceFrom": {results: [][]pkey{{argSelf(0)}}},
	"pluto/rt.ReleaseValues":  {},
	"pluto/rt.RetainValues":   {},
	"pluto/rt.ZeroValues":     {},
	"pluto/rt.NeedsDrop":      {},
	"pluto/rt.HoldsStrings":   {},
	"pluto/rt.MustCopy":       {},
	"pluto/rt.StringControl":  {},
	"pluto/rt.KnownASCII":     {},
}

// intrinsicSummary returns the summary of fn if the compiler expands it
// inline, from intrinsicEffects.
func (a *analyzer) intrinsicSummary(fn *function) (summary, bool) {
	if fn.obj == nil || fn.obj.Pkg() == nil || fn.sig.Recv() != nil {
		return summary{}, false
	}
	eff, ok := intrinsicEffects[fn.obj.Pkg().Path()+"."+fn.obj.Name()]
	if !ok {
		return summary{}, false
	}
	var s summary
	pos := shortPos(a.fset.Position(fn.decl.Pos()))
	for _, st := range eff.stores {
		s.addStore(edge{src: st.src, dst: st.dst}, &reason{text: fmt.Sprintf("stores %s (%s)", st.what, pos)})
	}
	for i, keys := range eff.results {
		for _, k := range keys {
			s.addResult(i, k, "")
		}
	}
	return s, true
}
