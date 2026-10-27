// Translated by go2pu from internal/escape/fields.go at the go-cutover tag; edited by hand since.

package escape

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
)

// Heap-only fields.
//
// A pointer loaded from a parameter's memory could point anywhere the
// caller's pointers do, so storing it into the heap makes the parameter
// escape. For many fields that is too pessimistic: an arena's current
// chunk, a list's next node or a map's table only ever hold memory from an
// allocator. A struct field is heap-only when, in the whole program, every
// value stored in it (by an assignment to the field, or in a composite
// literal) holds heap pointers only, and its address is never taken, nor
// that of anything inside it, so that nothing writes it except through the
// field. Loading a heap-only field yields the heap.
//
// The analysis starts by assuming every field is heap-only and withdraws
// the assumption for a field when it sees anything else stored there; the
// summaries are then computed again, until nothing changes. Writes through
// memory reinterpreted with unsafe.Pointer are not seen: like the rest of
// unsafe, they are the program's responsibility (spec section 20.1).

// fieldKey returns the field a selection or a struct type's field i
// refers to, the same for every instance of a generic type.
func fieldKey(v *types2.Var) *types2.Var { return v.Origin() }

// scanFields finds the fields whose address is taken, directly or by
// taking the address of something inside them.
func (a *analyzer) scanFields() {
	a.addrField = map[*types2.Var]bool{}
	a.nonHeap = map[*types2.Var]bool{}
	a.fieldDeps = map[*types2.Var]map[paramRef]bool{}
	a.paramNonHeap = map[paramRef]bool{}
	// The fields of a struct type that has no name are a variable of their own
	// in every place the type is written: identical types do not share them,
	// so what is stored in one cannot be told from what is read from another.
	a.namedField = map[*types2.Var]bool{}
	for _, obj := range a.defs {
		tn, _ := obj.(*types2.TypeName)
		if tn == nil || tn.IsAlias() {
			continue
		}
		if n, ok := tn.Type().(*types2.Named); ok {
			if st, ok := n.Underlying().(*types2.Struct); ok {
				for i := 0; i < st.NumFields(); i++ {
					a.namedField[fieldKey(st.Field(i))] = true
				}
			}
		}
	}
	for _, p := range a.prog.Packages {
		for _, f := range p.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.UnaryExpr:
					if n.Op == token.AND {
						a.takeAddr(n.X)
					}
				case *ast.SliceExpr:
					if _, ok := underlying(a.typeOf(n.X)).(*types2.Array); ok {
						a.takeAddr(n.X)
					}
				case *ast.SelectorExpr:
					a.implicitAddr(n)
				}
				return true
			})
		}
	}
}

func (a *analyzer) typeOf(e ast.Expr) types2.Type {
	if tv, ok := a.types[e]; ok {
		return tv.Type
	}
	return nil
}

// takeAddr records that the address of e, or of something inside it, is
// taken: every field e is part of, up to the first pointer it is reached
// through, may be written through that address.
func (a *analyzer) takeAddr(e ast.Expr) {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.SelectorExpr:
			sel := a.selections[x]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return
			}
			a.fieldsOnPath(a.typeOf(x.X), sel.Index(), true)
			if pointerElem(a.typeOf(x.X)) != nil {
				return
			}
			e = x.X
		case *ast.IndexExpr:
			if _, ok := underlying(a.typeOf(x.X)).(*types2.Array); !ok {
				return
			}
			e = x.X
		default:
			return
		}
	}
}

// fieldsOnPath marks the fields along a selection path from a value of
// type T as address-taken, stopping where the path goes through a pointer.
// With all, the last field is marked too.
func (a *analyzer) fieldsOnPath(T types2.Type, path []int, all bool) {
	if el := pointerElem(T); el != nil {
		T = el
	}
	// Only the fields after the last pointer on the path contain the
	// address taken.
	var marks []*types2.Var
	for i, idx := range path {
		s, ok := underlying(T).(*types2.Struct)
		if !ok || idx >= s.NumFields() {
			return
		}
		f := s.Field(idx)
		if i < len(path)-1 || all {
			marks = compat.Append(marks, f)
		}
		T = f.Type()
		if el := pointerElem(T); el != nil && i < len(path)-1 {
			marks, T = nil, el
		}
	}
	for _, f := range marks {
		a.addrField[fieldKey(f)] = true
	}
}

// implicitAddr handles x.M with a pointer method on an addressable x,
// which takes &x, and promoted methods, which take the address of the
// embedded fields on the way.
func (a *analyzer) implicitAddr(e *ast.SelectorExpr) {
	sel := a.selections[e]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return
	}
	fn, _ := sel.Obj().(*types2.Func)
	if fn == nil {
		return
	}
	recv := fn.Type().(*types2.Signature).Recv()
	if recv == nil || pointerElem(recv.Type()) == nil {
		return
	}
	viaPointer := pointerElem(a.typeOf(e.X)) != nil
	if len(sel.Index()) > 1 {
		emb := sel.Index()[:len(sel.Index())-1]
		T := a.typeOf(e.X)
		for _, idx := range emb {
			if el := pointerElem(T); el != nil {
				T, viaPointer = el, true
			}
			s, ok := underlying(T).(*types2.Struct)
			if !ok || idx >= s.NumFields() {
				return
			}
			T = s.Field(idx).Type()
		}
		if pointerElem(T) != nil {
			return // the receiver is the embedded pointer itself
		}
		a.fieldsOnPath(a.typeOf(e.X), emb, true)
	}
	if !viaPointer {
		a.takeAddr(e.X)
	}
}

// isHeapField reports whether loading field f yields only heap pointers.
func (a *analyzer) isHeapField(f *types2.Var) bool {
	k := fieldKey(f)
	if !a.namedField[k] || a.addrField[k] || a.nonHeap[k] || containsTypeParam(f.Type()) {
		return false
	}
	for p := range a.fieldDeps[k] {
		if a.paramNonHeap[p] {
			return false
		}
	}
	return true
}

// A paramRef names parameter idx of a function (receiver first).
type paramRef struct {
	fn  *function
	idx int
}

// storeField records that v is stored in field f, and in the fields of
// value type that contain it (outer, innermost first). A parameter stored
// as it is counts as heap memory if every call passes heap memory for it
// (passArgs).
func (a *analyzer) storeField(f *types2.Var, v tset, outer []*types2.Var) {
	var deps []paramRef
	heap := true
	for _, t := range v {
		switch {
		case t.kind == tHeap:
		case t.kind == tParam && t.path == "" && !t.ge && t.sc.fn != nil && t.sc.fn.passable():
			deps = compat.Append(deps, paramRef{t.sc.fn, t.idx})
		default:
			heap = false
		}
	}
	for _, g := range compat.Append([]*types2.Var{f}, outer...) {
		k := fieldKey(g)
		if !heap {
			if !a.nonHeap[k] {
				a.nonHeap[k] = true
				a.ver.field++
				a.grew = true
			}
			continue
		}
		for _, p := range deps {
			if a.fieldDeps[k] == nil {
				a.fieldDeps[k] = map[paramRef]bool{}
			}
			if !a.fieldDeps[k][p] {
				a.fieldDeps[k][p] = true
				a.ver.field++
				a.grew = true
			}
		}
	}
}

// passable reports whether every call of f is seen by the analysis, so
// that what its parameters hold is known from the calls (passArgs).
// Functions called from outside Plutonium (exported, interrupt handlers)
// and from task statements are not.
func (f *function) passable() bool { return !f.external }

// passArgs records what a call passes for f's parameters: a parameter
// given anything but heap memory loses its heap-only standing.
func (a *analyzer) passArgs(f *function, args []tset) {
	for i, arg := range args {
		p := paramRef{f, i}
		if a.paramNonHeap[p] {
			continue
		}
		for _, t := range arg {
			if t.kind != tHeap {
				a.paramNonHeap[p] = true
				a.ver.field++
				a.grew = true
				break
			}
		}
	}
}

// fieldOf returns the field a selector expression selects, or nil.
func (w *walker) fieldOf(e ast.Expr) *types2.Var {
	se, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	sel := w.a.selections[se]
	if sel == nil || sel.Kind() != types2.FieldVal {
		return nil
	}
	f, _ := sel.Obj().(*types2.Var)
	return f
}

// enclosingFields returns the fields of value type that contain the place
// lhs is inside, innermost first, up to the first pointer: the embedded
// fields a promoted field is reached through, and the fields lhs is
// written inside of.
func (w *walker) enclosingFields(lhs ast.Expr) []*types2.Var {
	var out []*types2.Var
	if se, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok {
		if sel := w.a.selections[se]; sel != nil && sel.Kind() == types2.FieldVal && len(sel.Index()) > 1 {
			T := w.typeOf(se.X)
			if el := pointerElem(T); el != nil {
				T = el
			}
			var path []*types2.Var
			for _, idx := range sel.Index()[:len(sel.Index())-1] {
				s, ok := underlying(T).(*types2.Struct)
				if !ok || idx >= s.NumFields() {
					break
				}
				f := s.Field(idx)
				if el := pointerElem(f.Type()); el != nil {
					// What follows is in another object.
					path, T = nil, el
					continue
				}
				path = compat.Append(path, f)
				T = f.Type()
			}
			for i := len(path) - 1; i >= 0; i-- {
				out = compat.Append(out, path[i])
			}
		}
	}
	e := lhs
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.SelectorExpr:
			if pointerElem(w.typeOf(x.X)) != nil {
				return out
			}
			if f := w.fieldOf(x.X); f != nil {
				out = compat.Append(out, f)
			}
			e = x.X
		case *ast.IndexExpr:
			if _, ok := underlying(w.typeOf(x.X)).(*types2.Array); !ok {
				return out
			}
			if f := w.fieldOf(x.X); f != nil {
				out = compat.Append(out, f)
			}
			e = x.X
		default:
			return out
		}
	}
}

// structField returns the field of s that element i of a composite literal
// of s sets: the one key names, or field i if there are no keys. It returns
// nil if s is nil or there is no such field.
func structField(s *types2.Struct, key *ast.Ident, i int) *types2.Var {
	if s == nil {
		return nil
	}
	if key != nil {
		for j := 0; j < s.NumFields(); j++ {
			if s.Field(j).Name() == key.Name {
				return s.Field(j)
			}
		}
		return nil
	}
	if i < s.NumFields() {
		return s.Field(i)
	}
	return nil
}

// storeConvFields records the stores a conversion to a struct type T makes
// into T's fields: each holds what the same field of the value converted
// does (v, told apart by field), and so do the fields of its fields.
func (a *analyzer) storeConvFields(T types2.Type, v tset, depth int) {
	s, ok := underlying(T).(*types2.Struct)
	if !ok || depth > maxFieldNest {
		return
	}
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		if !hasPointers(f.Type()) {
			continue
		}
		fv := selectField(v, i)
		a.storeField(f, fv, nil)
		a.storeConvFields(f.Type(), fv, depth+1)
	}
}
