// Translated by go2pu from internal/escape2/cache/ids.go at the go-cutover tag; edited by hand since.

package cache

import (
	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"pluto/slices"
	"pluto/strings"
)

// Stable function IDs. A FuncID is an index that changes whenever a function
// is added anywhere, and a token.Pos changes whenever a line moves, so a file
// names functions by what they are:
//
//   - a declared function or method by its full name, with package paths
//     ("(*pluto/fmt.pp).printArg");
//   - a package's initialization by "init " and the package path;
//   - a function literal, the body of a range over a function, a task thunk
//     and a deferred record by the declared function (or initialization)
//     whose source holds them, their kind and their rank among the
//     functions of that kind there in source order ("pkg.f lit#2"), so
//     that an edit of another function leaves them alone;
//   - any other synthesized function by its kind, receiver type, method and
//     field path.
//
// Two functions with one name (local types of one name in two functions,
// or the drops of a generic type as the program refers to it) are told
// apart by their order in the program.

// qualifier writes packages by path, the same in every run.
func qualifier(p *types2.Package) string { return p.Path() }

func typeString(T types2.Type) string {
	if T == nil {
		return "-"
	}
	return types2.TypeString(T, qualifier)
}

func funcString(fn *types2.Func) string {
	if fn == nil {
		return "-"
	}
	return fn.FullName()
}

var synthNames = map[eir.SynthKind]string{
	eir.SynthPromoted: "promoted", eir.SynthPtrWrapper: "ptrwrapper", eir.SynthItabAdapter: "itab",
	eir.SynthMethodValue: "methodvalue", eir.SynthMethodExpr: "methodexpr", eir.SynthIfaceMethodValue: "ifacemethodvalue",
	eir.SynthVariant: "variant", eir.SynthDropGlue: "drop", eir.SynthTaskThunk: "task", eir.SynthDeferRecord: "defer",
}

func synthName(k eir.SynthKind) string {
	if n, ok := synthNames[k]; ok {
		return n
	}
	return compat.Sprintfb("synth%d", compat.ArgU(uint64(k)))
}

// nested is a function named by where its source is.
type nested struct {
	id   eir.FuncID
	kind string
	pos  token.Pos
	tail string // what follows the rank
}

// stableIDs returns the stable ID of every function of p, "" for one that
// has none.
func stableIDs(p *eir.Program) []string {
	ids := compat.Make[string](len(p.Funcs))
	// Declared functions and initializations, by position, to find what
	// holds a nested function.
	type span struct {
		pos, end token.Pos
		name     string
	}
	var spans []span
	initOf := map[*types2.Package]string{}
	var nest []nested
	for _, f := range p.Funcs {
		k := f.Key
		switch {
		case k.Obj != nil:
			ids[f.ID] = funcString(k.Obj)
			spans = compat.Append(spans, span{f.Pos, f.End, ids[f.ID]})
		case k.Init != nil:
			ids[f.ID] = compat.Concat("init ", k.Init.Path())
			initOf[k.Init] = ids[f.ID]
		case k.Lit != nil:
			nest = compat.Append(nest, nested{id: f.ID, kind: "lit", pos: k.Lit.Pos()})
		case k.Range != nil:
			nest = compat.Append(nest, nested{id: f.ID, kind: "range", pos: k.Range.Pos()})
		case k.Synth != nil && k.Synth.Site != nil:
			s := k.Synth
			nest = compat.Append(nest, nested{id: f.ID, kind: synthName(s.Kind), pos: s.Site.Pos(),
				tail: compat.Concat(" ", typeString(s.Recv), " ", funcString(s.Method), compat.Sprint(s.Path))})
		case k.Synth != nil:
			s := k.Synth
			ids[f.ID] = compat.Sprintf("%s %s %s %v", synthName(s.Kind), typeString(s.Recv), funcString(s.Method), s.Path)
		}
	}
	// The declared function holding pos (declarations do not nest, and
	// the files of a program have disjoint positions); failing that, the
	// package's initialization.
	slices.SortFunc(spans, func(a, b span) int { return cmpPos(a.pos, b.pos) })
	holder := func(f *eir.Func, pos token.Pos) string {
		// The last declaration that starts at or before pos.
		i, _ := slices.BinarySearchFunc(spans, pos+1, func(s span, p token.Pos) int { return cmpPos(s.pos, p) })
		if i > 0 && pos < spans[i-1].end {
			return spans[i-1].name
		}
		if n, ok := initOf[f.Pkg]; ok {
			return n
		}
		if f.Pkg != nil {
			return compat.Concat("init ", f.Pkg.Path())
		}
		return ""
	}
	type group struct{ holder, kind string }
	groups := map[group][]nested{}
	for _, n := range nest {
		h := holder(p.Funcs[n.id], n.pos)
		if h == "" {
			continue
		}
		g := group{h, n.kind}
		groups[g] = compat.Append(groups[g], n)
	}
	for g, ns := range groups {
		slices.SortStableFunc(ns, func(a, b nested) int {
			if c := cmpPos(a.pos, b.pos); c != 0 {
				return c
			}
			return strings.Compare(a.tail, b.tail)
		})
		for i, n := range ns {
			ids[n.id] = compat.Sprintfb("%s %s#%d%s", compat.ArgS(g.holder), compat.ArgS(g.kind), compat.ArgI(int64(i)), compat.ArgS(n.tail))
		}
	}
	// Functions of one name are told apart by their rank in the program,
	// which holds while the program's functions do not change around them;
	// when they do, an entry of one may be offered for another, which the
	// EIR digest then refuses (and the check would).
	seen := map[string]int{}
	for id, s := range ids {
		if s == "" {
			continue
		}
		if n := seen[s]; n > 0 {
			ids[id] = compat.Sprintfb("%s dup#%d", compat.ArgS(s), compat.ArgI(int64(n)))
		}
		seen[s]++
	}
	return ids
}

func cmpPos(a, b token.Pos) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
