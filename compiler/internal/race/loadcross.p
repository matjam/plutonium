package race

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// Loads that cross (language spec section 19.3, Crossings case 5). A party
// that loads a view from shared memory once tasks may run gets a view that
// another party may also hold, wherever the view was stored: the load is a
// crossing, and rule 2 applies to what the view points to. The analysis
// finds these loads among the accesses of each party (access.view), so
// a load in a callee counts where the callee runs, and a load before the
// start of sharing does not count.
//
// What a view loaded from a package variable points to is what code stored
// there: the stores are recorded (gstore), and the view is judged by them.
// A composite literal whose address a package initializer stores is a
// static object (section 19.3, Memory and data) as long as no code moves,
// drops or overwrites it once tasks may run; a view of one is judged as a
// view of the package variable, and the free condition of the type rule
// does not count against it. A variable whose address is taken may be
// stored through a pointer, and a store of a value the analysis cannot
// follow settles nothing: a view loaded from either is judged by its type
// alone.

// A gstore is a value stored in a package variable, at path within it.
type gstore struct {
	path    string
	vals    origins
	unknown bool // a value that holds a view, with no origins the analysis knows
}

// noteGlobalStore records that val, of type T, is stored at place p of a
// package variable.
func (a *analyzer) noteGlobalStore(p vpath, val origins, T types2.Type) {
	v := p.root.v
	p.path = stripParts(p.path) // which elements, for the check of scoped tasks only
	unknown := len(val) == 0 && a.holdsView(T, 0)
	for _, s := range a.gstores[v] {
		if s.path == p.path && s.unknown == unknown && sameOrigins(s.vals, val) {
			return
		}
	}
	a.gstores[v] = compat.Append(a.gstores[v], gstore{path: p.path, vals: val, unknown: unknown})
}

func sameOrigins(a, b origins) bool {
	if len(a) != len(b) {
		return false
	}
	for _, o := range b {
		if !a.has(o) {
			return false
		}
	}
	return true
}

// storedAt returns the origins of the values that may be at place p of a
// package variable, from the stores of the variable, and whether they are
// all known. A composite literal a package initializer stored stands for
// itself as p: what it points to is reached through p.
func (a *analyzer) storedAt(p vpath) (origins, bool) {
	v := p.root.v
	if p.root.kind != rGlobal || strings.ContainsAny(p.path, "*%$&#@") || a.addrGlobals[v] {
		return nil, false
	}
	var out origins
	for _, s := range a.gstores[v] {
		var vals origins
		switch {
		case s.path == p.path:
			vals = s.vals
		case prefixOf(s.path, p.path):
			vals = s.vals
			for _, st := range steps(strings.SliceFrom(p.path, len(s.path))) {
				switch {
				case strings.HasPrefix(st, "."):
					vals = vals.field(strings.SliceFrom(st, 1))
				case st == "[]":
				default:
					return nil, false
				}
			}
		case prefixOf(p.path, s.path):
			// A part of the value loaded was stored by itself.
			return nil, false
		default:
			continue
		}
		if s.unknown {
			return nil, false
		}
		for _, o := range vals {
			if o.vp.root.kind == rFresh && a.initLits[o.vp.root.node] && o.vp.path == "&" && o.tag == "" {
				o = origin{vp: p}
			}
			out = origins(compat.Append(out, o))
		}
	}
	return out.uniq(), true
}

// resolveVals replaces each origin of vals that is the value stored in a
// package variable by the values stored there. It returns nil when one of
// them cannot be followed: the view is then judged by its type.
func (a *analyzer) resolveVals(vals origins) origins {
	var out origins
	for _, o := range vals {
		if o.vp.root.kind != rGlobal || o.vp.isAddr() || strings.ContainsAny(o.vp.path, "*%$#@") || o.tag != "" {
			out = origins(compat.Append(out, o))
			continue
		}
		got, ok := a.storedAt(o.vp)
		if !ok {
			return nil
		}
		for _, g := range got {
			if g.vp.root.kind == rGlobal && g.vp.root.v == o.vp.root.v && g.vp.path == o.vp.path {
				out = origins(compat.Append(out, o))
				continue
			}
			out = origins(compat.Append(out, g))
		}
	}
	return out.uniq()
}

// staticVar reports whether every value stored at place p of a package
// variable is a static object.
func (a *analyzer) staticVar(p vpath) bool {
	v := p.root.v
	if p.root.kind != rGlobal || a.addrGlobals[v] {
		return false
	}
	found := false
	for _, s := range a.gstores[v] {
		if !prefixOf(s.path, p.path) && !prefixOf(p.path, s.path) {
			continue
		}
		if s.path != p.path || s.unknown {
			return false
		}
		for _, o := range s.vals {
			if o.vp.root.kind != rFresh || !a.statics[o.vp.root.node] || o.vp.path != "&" || o.tag != "" {
				return false
			}
			found = true
		}
	}
	return found
}

// allStatic reports whether every origin of vals is a view of a static
// object.
func (a *analyzer) allStatic(vals origins) bool {
	if len(vals) == 0 {
		return false
	}
	for _, o := range vals {
		switch {
		case o.vp.root.kind == rFresh && a.statics[o.vp.root.node] && o.vp.path == "&":
		case o.vp.root.kind == rGlobal && !o.vp.isAddr() && !strings.ContainsAny(o.vp.path, "*%$#@") && a.staticVar(o.vp):
		default:
			return false
		}
	}
	return true
}

// freeProblem returns a free, drop, move or overwrite of an object of
// struct type E once tasks may run, unless every origin of vals is a
// static object, which other objects' frees do not concern (section 19.3,
// Rule 3: the free condition).
func (a *analyzer) freeProblem(E types2.Type, vals origins) *writeSite {
	s := a.freedTypes[typeKey(E)]
	if s == nil || a.allStatic(vals) {
		return nil
	}
	return s
}

// finishShared works out, from the accesses of every party once tasks may
// run, the struct types of the objects freed, dropped, moved or overwritten
// as a whole, and the static objects: the composite literals package
// initializers stored that no such write reaches.
func (a *analyzer) finishShared(groups []*group) {
	a.freedTypes = map[string]*writeSite{}
	a.statics = map[ast.Node]bool{}
	for n := range a.initLits {
		a.statics[n] = true
	}
	var bulk *writeSite
	for _, g := range groups {
		for _, x := range g.accesses {
			if x.write && isAllocMem(x.place) && bulk == nil {
				bulk = pu.New(writeSite{pos: x.pos, fn: a.funcAt(x.pos), expr: "an object of the type"})
			}
			if !x.write || (!x.free && !strings.HasSuffix(x.place.path, "*")) {
				continue
			}
			site := pu.New(writeSite{pos: x.pos, fn: a.funcAt(x.pos), expr: "an object of the type"})
			if x.free {
				a.noteFreed(x.typ, site, 0)
			} else if isAggregate(x.typ) {
				if a.freedTypes[typeKey(x.typ)] == nil {
					a.freedTypes[typeKey(x.typ)] = site
				}
			}
			a.unstatic(x)
		}
	}
	if bulk != nil {
		a.bulkFreed(bulk)
	}
}

// bulkFreed records that an allocator's Reset, Release or Drop once tasks
// run frees objects of every type the program may allocate from an
// allocator (section 19.3, Frees are writes): the compiler does not tell
// which allocator an object came from, so every such type counts.
func (a *analyzer) bulkFreed(site *writeSite) {
	seen := map[types2.Type]bool{}
	var note func(T types2.Type, depth int)
	note = func(T types2.Type, depth int) {
		if T == nil || seen[T] || depth > 4 {
			return
		}
		seen[T] = true
		if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
			for _, arg := range a.tpArgs[tp] {
				note(arg, depth+1)
			}
			return
		}
		if _, ok := a.freedTypes[typeKey(T)]; !ok {
			a.noteFreed(T, site, 0)
		}
	}
	for _, T := range a.arenaTypes {
		note(T, 0)
	}
}

// arenaAllocFunc reports whether f allocates an object of its type argument
// from an allocator it is given: alloc.New, Owned, Shared and Synced, and
// the New and Slice methods of pluto/arena's arenas.
func arenaAllocFunc(f *types2.Func) bool {
	if f.Pkg() == nil {
		return false
	}
	switch f.Pkg().Path() {
	case "pluto/alloc":
		switch f.Name() {
		case "New", "Owned", "Shared", "Synced":
			return true
		}
	case "pluto/arena":
		switch f.Name() {
		case "New", "Slice":
			return true
		}
	}
	return false
}

// unstatic drops the static objects that write x may free, move or
// overwrite: those stored in the package variable it goes through, or,
// when it does not go through one, every one of its type.
func (a *analyzer) unstatic(x *access) {
	p := x.place
	if p.root.kind == rGlobal && !strings.ContainsAny(p.path, "$") {
		i := strings.IndexAny(p.path, "*%#")
		if i < 0 {
			return // the variable itself: what it pointed to stays
		}
		if rest := strings.SliceFrom(p.path, i); p.path[i] == '*' && rest != "*" && rest != "*#" {
			// Memory the object owns, or an object reached through it:
			// the object itself stays.
			return
		}
		for _, s := range a.gstores[p.root.v] {
			if !prefixOf(s.path, strings.Slice(p.path, 0, i)) {
				continue
			}
			for _, o := range s.vals {
				if o.vp.root.kind == rFresh {
					delete(a.statics, o.vp.root.node)
				}
			}
		}
		return
	}
	for n := range a.statics {
		if lit, ok := n.(*ast.CompositeLit); ok && a.sameObjectType(a.typeOf(lit), x.typ) {
			delete(a.statics, n)
		}
	}
}

// sameObjectType reports whether an object of type T may be what a write
// of a value of type U frees.
func (a *analyzer) sameObjectType(T, U types2.Type) bool {
	if T == nil || U == nil {
		return true
	}
	if p := pointerElem(T); p != nil {
		T = p
	}
	k := typeKey(T)
	seen := map[string]*writeSite{}
	saved := a.freedTypes
	a.freedTypes = seen
	a.noteFreed(U, nil, 0)
	a.freedTypes = saved
	_, ok := seen[k]
	return ok || typeKey(U) == k
}

// noteFreed records that a free or drop of a value of type T frees objects
// of the struct types it is or owns.
func (a *analyzer) noteFreed(T types2.Type, site *writeSite, depth int) {
	if T == nil || depth > 8 {
		return
	}
	switch u := underlying(T).(type) {
	case *types2.Struct:
		k := typeKey(T)
		if _, ok := a.freedTypes[k]; ok {
			return
		}
		a.freedTypes[k] = site
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			switch {
			case a.ownsField(T, f):
				a.noteFreed(viewElem(f.Type()), site, depth+1)
			case dropsMemory(f.Type()):
				a.noteFreed(f.Type(), site, depth+1)
			}
		}
	case *types2.Array:
		a.noteFreed(u.Elem(), site, depth+1)
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapOwned, types2.WrapVolatile, types2.WrapAligned:
			a.noteFreed(u.Elem(), site, depth+1)
		}
	}
}

// loadCrossings returns the loads of views from shared memory that each
// party makes once tasks may run.
//
// With one party and no task that runs more than once, no memory is shared
// and no load is a crossing. The tasks of a Scope are part of the party
// that starts them: their uses of views are checked by the partition rules
// (section 19.4).
func (a *analyzer) loadCrossings(groups []*group) []*crossing {
	var out []*crossing
	if len(groups) == 1 && !groups[0].multi {
		return nil
	}
	seen := map[string]bool{}
	for _, g := range groups {
		xs := g.accesses
		// Loads from per-CPU variables are kept with the uses section 20.9
		// checks, but cross as loads from any package variable do.
		for _, e := range g.cpu {
			if e.x.view && !e.x.pre {
				xs = compat.Append(xs, e.x)
			}
		}
		for _, x := range xs {
			if !x.view || x.write || x.typ == nil {
				continue
			}
			k := compat.Concat(itoa(int(x.pos)), "|", x.place.key())
			if seen[k] {
				continue
			}
			seen[k] = true
			name := "a view"
			if x.place.root.kind == rGlobal && x.place.root.v != nil {
				name = a.placeText(x.place)
			}
			p := x.place
			p.path = stripParts(p.path)
			out = compat.Append(out, pu.New(crossing{pos: x.pos, what: compat.Concat("the value loaded from ", name), typ: x.typ, vals: compat.Clone(origins{{vp: p}}), load: true, name: name}))
		}
	}
	return out
}
