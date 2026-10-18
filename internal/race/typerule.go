package race

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// The type rule. A view may cross to another task when every object of
// the struct type it points to is safe to use from several tasks at once:
// nothing overwrites a whole object through a view, and each field is
// either never changed once the object is reachable from anywhere but the
// function that allocated it, or used only with atomic operations, or only
// while holding one lock of the same object (or one package-level lock),
// inferred from the accesses as for package variables. An immutable struct
// is the case with no changes; an object guarded by its own mutex, such as
// one with a sync.Mutex field next to the data it protects, is the other.

// A typeAccess is a use of a field of an object, in any function: which
// locks it holds (named relative to the object, or package-level ones) and
// whether it is atomic.
type typeAccess struct {
	write, atomic bool
	excl, shared  []string
	pos           token.Pos
	fn            string
	global        *types2.Var // the object is this package variable
}

// recordType records a use of field key of the object place p is in.
// Initialising uses of memory nobody else can reach yet are left out.
func (a *analyzer) recordType(key string, p vpath, write, atomic bool, locks lockset, pos token.Pos, fn string, initializing bool) {
	if initializing || p.root.kind == rLocal || p.root.kind == rParam {
		return
	}
	base := objectBase(p)
	x := &typeAccess{write: write, atomic: atomic, pos: pos, fn: fn}
	if p.root.kind == rGlobal && !strings.ContainsAny(base.path, "*%$#") {
		x.global = p.root.v
	}
	for _, l := range locks {
		var k string
		if l.place.root == base.root && prefixOf(base.path, l.place.path) && !strings.ContainsAny(l.place.path[len(base.path):], "*%$#") {
			k = "rel:" + l.place.path[len(base.path):]
		} else if g, ok := globalLock(l); ok {
			k = "glob:" + g
		}
		if k == "" {
			continue
		}
		if l.excl {
			x.excl = append(x.excl, k)
		} else {
			x.shared = append(x.shared, k)
		}
	}
	sort.Strings(x.excl)
	sort.Strings(x.shared)
	gk := ""
	if x.global != nil {
		gk = x.global.Name()
	}
	dk := fmt.Sprintf("%s|%v|%v|%v|%v|%d|%s", key, write, atomic, x.excl, x.shared, pos, gk)
	if a.typeSeen[dk] {
		return
	}
	a.typeSeen[dk] = true
	a.typeAcc[key] = append(a.typeAcc[key], x)
}

// funcAt names the declared function whose body holds pos, for messages.
func (a *analyzer) funcAt(pos token.Pos) string {
	best := ""
	for _, f := range a.funcs {
		if f.decl != nil && f.decl.Pos() <= pos && pos < f.decl.End() {
			best = f.name
		}
	}
	if best == "" {
		return "package initialization"
	}
	return best
}

// fieldProblem checks the type rule for field key. It returns an access at
// fault and a write it may meet, or nils.
func (a *analyzer) fieldProblem(key string) (bad, write *typeAccess) {
	var xs []*typeAccess
	for _, x := range a.typeAcc[key] {
		if x.global != nil && !a.addrGlobals[x.global] {
			continue // no view reaches it: the package variable rule covers it
		}
		xs = append(xs, x)
	}
	var g map[string]bool
	for _, x := range xs {
		if !x.write {
			continue
		}
		xg := map[string]bool{}
		if x.atomic {
			xg["atomic"] = true
		}
		for _, k := range x.excl {
			xg[k] = true
		}
		if g == nil {
			g = xg
		} else {
			g = intersect(g, xg)
		}
		if len(g) == 0 {
			return x, x
		}
		write = x
	}
	if g == nil {
		return nil, nil // never changed once reachable
	}
	for _, x := range xs {
		held := x.atomic && g["atomic"]
		for _, k := range x.excl {
			held = held || g[k]
		}
		for _, k := range x.shared {
			held = held || g[k]
		}
		if !held {
			return x, write
		}
	}
	return nil, nil
}

// typeProblem checks the type rule for struct type E and returns where it
// fails, with an explanation when the failing use is not itself a write.
func (a *analyzer) typeProblem(E types2.Type) (*writeSite, string) {
	return a.typeProblemIn(E, map[types2.Type]bool{})
}

func (a *analyzer) typeProblemIn(E types2.Type, seen map[types2.Type]bool) (*writeSite, string) {
	if seen[E] {
		return nil, ""
	}
	seen[E] = true
	if a.selfSynchronized(E) {
		return nil, ""
	}
	if s := a.viewWrites["whole:"+typeKey(E)]; s != nil {
		return s, ""
	}
	st, ok := underlying(E).(*types2.Struct)
	if !ok {
		if arr, ok := underlying(E).(*types2.Array); ok && isAggregate(arr.Elem()) {
			return a.typeProblemIn(arr.Elem(), seen)
		}
		return nil, ""
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if isSyncType(f.Type()) {
			continue
		}
		key := "field:" + typeKey(E) + "." + f.Name()
		if bad, w := a.fieldProblem(key); bad != nil {
			site := &writeSite{pos: bad.pos, fn: a.funcAt(bad.pos), expr: "its field " + f.Name()}
			if !bad.write && w != nil {
				return site, fmt.Sprintf("Its field %s is changed at %s (in %s) and read at %s (in %s) without a lock that every change holds.", f.Name(), a.short(w.pos), a.funcAt(w.pos), a.short(bad.pos), a.funcAt(bad.pos))
			}
			return site, ""
		}
		if isAggregate(f.Type()) {
			if s, why := a.typeProblemIn(f.Type(), seen); s != nil {
				return s, why
			}
		}
	}
	return nil, ""
}
