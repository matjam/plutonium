// Translated by go2pu from internal/escape/deps.go at the go-cutover tag; edited by hand since.

package escape

// What a function's walk depends on.
//
// Walking a function again, when nothing it read has changed since the last
// walk, finds nothing new: every summary and fact only grows, and a walk only
// adds what its inputs lead to. The passes that compute the summaries
// therefore skip such a function. A walk reads two kinds of input that
// change: the summary of a function it calls (and the facts kept beside it;
// sum.ver counts the changes), and the facts about the whole program that
// choose the functions a dynamic call may reach and tell which fields only
// hold heap pointers (globalVers). The rest comes from the source and the
// types.

// globalVers count the changes to the facts that are not a function's own.
type globalVers struct {
	conv  uint32 // types converted to an interface (the methods a call may reach)
	fv    uint32 // functions used as values
	field uint32 // heap-only fields
	lit   uint32 // function literals seen
}

// dep records that the function being analysed reads g, and reports whether
// g is a function (so that it can be used in a condition).
func (a *analyzer) dep(g *function) bool {
	if g == nil {
		return false
	}
	if a.deps != nil {
		if _, ok := a.deps[g]; !ok {
			a.deps[g] = g.sum.ver
		}
	}
	return true
}

// unchanged reports whether nothing f read in its last walk has changed.
func (a *analyzer) unchanged(f *function) bool {
	if !f.analyzed || f.depsVer != a.ver {
		return false
	}
	for g, v := range f.deps {
		if g.sum.ver != v {
			return false
		}
	}
	return true
}
