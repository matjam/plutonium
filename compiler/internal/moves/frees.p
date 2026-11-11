package moves

import (
	"compiler/compat"
	"compiler/internal/escape2/contract"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"pluto/strconv"
	"pluto/strings"
)

// Through which inputs a function may free memory or move an owner out
// (language spec section 18.3, A local owner's memory, and "How precise
// the frame check is"). A view of a local owner's memory ends at a call
// that takes the owner, or memory it owns, by pointer and may free memory
// the view looks at or move an owner out of it:
//
//	s := v.Slice()
//	v.Push(1)     // Push may free the old array
//	println(s[0]) // E3313
//
// frees records, for each function with a body and each of its inputs (the
// receiver first), the statement or call by which the function may do
// that through memory reached from the input. A function frees through an
// input when, through a pointer loaded from the input (keepScan.sets):
//
//   - it assigns to a place whose type holds an owner or a counted box,
//     which drops the old value;
//   - it moves an owner out of such a place with take, or drops elements
//     with clear or delete;
//   - it passes such a pointer to an allocator's Free, to the Drop method
//     of an owner, to Pop, Reset or Drop of a ring, or to the runtime's
//     ReleaseValues or MoveValues (the source, unless the next statement
//     is a RetainValues of the same destination and count: the pair copies
//     the values, as CopyValues does);
//   - it passes the input, or memory loaded through it, to a function that
//     frees through the input it is given. This rule is followed through
//     any number of calls, to a fixed point, as keeps.p does.
//
//   - it passes the input, or memory loaded through it, to a call of a func
//     value whose candidates are not known, or of a function without a
//     body that the contract table does not describe (bodyless): the worst
//     case, when the argument's type reaches memory that may be freed
//     (reachesFreeing).
//
// A call's result holds what the escape analysis says it may derive from
// (keepScan.sets), so a free through a pointer a call returned is found.
// The other way round, a pin or view that only the freeing call made is
// not ended.

// findFrees computes c.frees, c.reclaims and c.asserts, after findKeeps.
func (c *checker) findFrees(funcs []keepFunc) {
	c.frees = c.summarize(funcs, func(s *keepScan, body *ast.BlockStmt, found func(in int, site keepSite), edge func(to ast.Node, j, in int)) {
		s.frees(body, found, edge)
	})
	c.reclaims = c.summarize(funcs, func(s *keepScan, body *ast.BlockStmt, found func(in int, site keepSite), edge func(to ast.Node, j, in int)) {
		s.reclaims(body, found, edge)
	})
	c.asserts = c.summarize(funcs, func(s *keepScan, body *ast.BlockStmt, found func(in int, site keepSite), edge func(to ast.Node, j, in int)) {
		s.assertsIn(body, found, edge)
	})
}

// assertsIn finds, in body, the type assertions and type switches on a
// value loaded from an input or that is one, the calls that pass an input
// on to a function that does (edge), and the calls whose callees are not
// known, or of a function without a body, given something that may hold
// an interface (the worst case). An assertion on an ordinary interface
// yields what was lent to it (the C2 ruling,
// docs/verification/2026-11-09-spec-rulings.md): a view of the variable
// lent.
func (s *keepScan) assertsIn(body *ast.BlockStmt, found func(in int, site keepSite), edge func(to ast.Node, j, in int)) {
	each := func(bits uint64, site keepSite) {
		for i := 0; i < maxInputs; i++ {
			if bits&bit(i) != 0 {
				found(i, site)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.TypeAssertExpr:
			o, h := s.sets(x.X)
			each(o|h, keepSite{pos: x.Pos(), text: exprText(x)})
		case *ast.CallExpr:
			tv := s.c.types[x.Fun]
			if tv.IsType() || tv.IsBuiltin() {
				return true
			}
			site := keepSite{pos: x.Pos(), text: exprText(x)}
			targets, known := s.c.keepTargets(x)
			worst := !known || s.c.bodyless(x)
			s.c.callArgs(x, func(key, val ast.Expr, j int) {
				o, h := s.sets(val)
				bits := o | h
				if bits == 0 {
					return
				}
				if worst && reachesFreeing(s.c.typeOf(val)) {
					each(bits, site)
				}
				for _, t := range targets {
					for i := 0; i < maxInputs; i++ {
						if bits&bit(i) != 0 {
							edge(t.node, j+t.shift, i)
						}
					}
				}
			})
		}
		return true
	})
}

// calleeAsserts reports whether a callee of call may assert or type-switch
// on what its argument key holds (c.asserts).
func (c *checker) calleeAsserts(call *ast.CallExpr, key ast.Expr) bool {
	targets, known := c.keepTargets(call)
	worst := !known || c.bodyless(call)
	found := false
	c.callArgs(call, func(k, val ast.Expr, j int) {
		if k != key || found {
			return
		}
		if worst && reachesFreeing(c.typeOf(val)) {
			found = true
			return
		}
		for _, t := range targets {
			fs := c.asserts[t.node]
			if j+t.shift < len(fs) && fs[j+t.shift].valid() {
				found = true
				return
			}
		}
	})
	return found
}

// summarize computes, for each function of funcs and each of its inputs,
// the first site scan finds in its body through that input, followed
// through the calls that pass the input on (edge) to a fixed point.
func (c *checker) summarize(funcs []keepFunc, scan func(s *keepScan, body *ast.BlockStmt, found func(in int, site keepSite), edge func(to ast.Node, j, in int))) map[ast.Node][]keepSite {
	sum := map[ast.Node][]keepSite{}
	into := map[ast.Node][]keepEdge{}
	type pending struct {
		fn ast.Node
		j  int
	}
	var work []pending
	for _, sc := range funcs {
		sum[sc.node] = compat.Make[keepSite](sc.n)
	}
	for _, sc := range funcs {
		node := sc.node
		// What the locals hold, call results included: q := id(p) holds
		// what p points to when the escape analysis says the result may
		// derive from p.
		scan(sc.scan, sc.body, func(in int, site keepSite) {
			if fs := sum[node]; in < len(fs) && !fs[in].valid() {
				fs[in] = site
				work = compat.Append(work, pending{fn: node, j: in})
			}
		}, func(to ast.Node, j, in int) {
			into[to] = compat.Append(into[to], keepEdge{from: node, in: in, j: j})
		})
	}
	for len(work) > 0 {
		w := work[len(work)-1]
		work = work[:len(work)-1]
		site := sum[w.fn][w.j]
		for _, e := range into[w.fn] {
			if fs := sum[e.from]; e.j == w.j && e.in < len(fs) && !fs[e.in].valid() {
				fs[e.in] = site
				work = compat.Append(work, pending{fn: e.from, j: e.in})
			}
		}
	}
	return sum
}

// reclaims finds, in body, the calls that may reset or release a checked
// arena reached from an input (section 18.9): Reset or Release with the
// arena, or a pointer to it, as the receiver; a call of a function that
// reclaims through what it is given (edge); and a call whose callees are
// not known, or of a function without a body, given something whose type
// may lead to an arena (reachesArena: the worst case, as in freeCall).
func (s *keepScan) reclaims(body *ast.BlockStmt, found func(in int, site keepSite), edge func(to ast.Node, j, in int)) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		tv := s.c.types[call.Fun]
		if tv.IsType() || tv.IsBuiltin() {
			return true
		}
		site := keepSite{pos: call.Pos(), text: exprText(call)}
		prim := s.c.reclaimPrimitive(call)
		targets, known := s.c.keepTargets(call)
		worst := !known || s.c.bodyless(call)
		s.c.callArgs(call, func(key, val ast.Expr, j int) {
			own, held := s.addrSets(val)
			if lentIface(s.c.typeOf(val)) {
				held = 0 // as in freeCall
			}
			bits := own | held
			if bits == 0 {
				return
			}
			if j == 0 && prim || worst && reachesArena(s.c.typeOf(val)) {
				for i := 0; i < maxInputs; i++ {
					if bits&bit(i) != 0 {
						found(i, site)
					}
				}
			}
			for _, t := range targets {
				for i := 0; i < maxInputs; i++ {
					if bits&bit(i) != 0 {
						edge(t.node, j+t.shift, i)
					}
				}
			}
		})
		return true
	})
}

// reclaimPrimitive reports whether call is Reset or Release of a checked
// arena, which reclaims through its receiver.
func (c *checker) reclaimPrimitive(call *ast.CallExpr) bool {
	se := funSelector(call.Fun)
	if se == nil || se.Sel.Name != "Reset" && se.Sel.Name != "Release" {
		return false
	}
	sel := c.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return false
	}
	T := sel.Recv()
	if p := pointerElem(T); p != nil {
		T = p
	}
	return isCheckedArena(T)
}

// calleeReclaims returns the call by which a callee of call may reset or
// release a checked arena reached from its argument key, or false.
func (c *checker) calleeReclaims(call *ast.CallExpr, key ast.Expr) (keepSite, bool) {
	prim := c.reclaimPrimitive(call)
	targets, known := c.keepTargets(call)
	worst := !known || c.bodyless(call)
	var site keepSite
	found := false
	c.callArgs(call, func(k, val ast.Expr, j int) {
		if k != key || found {
			return
		}
		if j == 0 && prim || worst && reachesArena(c.typeOf(val)) {
			site, found = keepSite{pos: call.Pos(), text: exprText(call)}, true
			return
		}
		for _, t := range targets {
			fs := c.reclaims[t.node]
			if j+t.shift < len(fs) && fs[j+t.shift].valid() {
				site, found = fs[j+t.shift], true
				return
			}
		}
	})
	return site, found
}

// A keepFunc is a function findKeeps scanned, for findFrees: its node, the
// number of its inputs, its body and the scan of its local variables.
type keepFunc struct {
	node ast.Node
	n    int
	body *ast.BlockStmt
	scan *keepScan
}

// through returns the inputs through which the place x is reached by a
// load (a pointer dereference, an index of a slice or a map): an
// assignment to x writes memory reached from them. A place in a local
// variable itself is reached through none.
func (s *keepScan) through(x ast.Expr) uint64 {
	for {
		switch e := x.(type) {
		case *ast.ParenExpr:
			x = e.X
			continue
		case *ast.SelectorExpr:
			sel := s.c.selections[e]
			if sel == nil || sel.Kind() != types2.FieldVal {
				return 0
			}
			if !sel.Indirect() {
				x = e.X
				continue
			}
			o, h := s.addrSets(e.X)
			return o | h
		case *ast.IndexExpr:
			T := s.c.typeOf(e.X)
			if T == nil {
				return 0
			}
			if _, ok := T.Underlying().(*types2.Array); ok {
				x = e.X
				continue
			}
			o, h := s.addrSets(e.X)
			return o | h
		case *ast.StarExpr:
			o, h := s.addrSets(e.X)
			return o | h
		}
		return 0
	}
}

// addrSets is sets, also through the unsafe functions that make a pointer
// from a slice or string (SliceData, StringData) or move one (Add).
func (s *keepScan) addrSets(e ast.Expr) (own, held uint64) {
	if call, ok := ast.Unparen(e).(*ast.CallExpr); ok && len(call.Args) > 0 {
		if tv := s.c.types[call.Fun]; tv.IsBuiltin() {
			var name string
			switch f := ast.Unparen(call.Fun).(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
			}
			switch name {
			case "SliceData", "StringData", "Add":
				return s.addrSets(call.Args[0])
			}
		}
		if tv := s.c.types[call.Fun]; tv.IsType() && len(call.Args) == 1 {
			return s.addrSets(call.Args[0])
		}
	}
	return s.sets(e)
}

// freeing reports whether a value of type T holds an owner or a counted
// box, so that replacing it may free memory a view looks at.
func freeing(T types2.Type) bool { return freeingSeen(T, map[types2.Type]bool{}) }

func freeingSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if bounds(T) {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if freeingSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types2.Array:
		return u.Len() > 0 && freeingSeen(u.Elem(), seen)
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapAligned, types2.WrapVolatile, types2.WrapRing:
			return freeingSeen(u.Elem(), seen)
		}
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && freeingSeen(v.Payload(), seen) {
				return true
			}
		}
	}
	return false
}

// elemFreeing reports whether the elements of the slice, array or map
// type T hold an owner or a counted box.
func elemFreeing(T types2.Type) bool {
	if T == nil {
		return false
	}
	switch u := T.Underlying().(type) {
	case *types2.Slice:
		return freeing(u.Elem())
	case *types2.Array:
		return freeing(u.Elem())
	case *types2.Map:
		return freeing(u.Elem())
	case *types2.Pointer:
		return elemFreeing(u.Elem())
	}
	return false
}

// frees finds, in body, the statements and calls that may free memory or
// move an owner out through an input (freed), and the inputs passed to
// functions that may do so through what they are given (edge).
func (s *keepScan) frees(body *ast.BlockStmt, freed func(in int, site keepSite), edge func(to ast.Node, j, in int)) {
	each := func(bits uint64, site keepSite) {
		for i := 0; i < maxInputs; i++ {
			if bits&bit(i) != 0 {
				freed(i, site)
			}
		}
	}
	copies := s.copyPairs(body)
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.ASSIGN {
				return true
			}
			for _, l := range x.Lhs {
				if bits := s.through(l); bits != 0 && freeing(s.c.typeOf(l)) {
					each(bits, keepSite{pos: x.Pos(), text: compat.Concat(exprText(l), " = ...")})
				}
			}
		case *ast.CallExpr:
			if !copies[x] {
				s.freeCall(x, each, edge)
			}
		}
		return true
	})
}

// copyPairs returns the calls of the runtime's MoveValues in body that the
// next statement makes a copy, by a RetainValues of the same destination
// and count: the source keeps its values and their references, so the
// pair frees nothing through it.
func (s *keepScan) copyPairs(body *ast.BlockStmt) map[*ast.CallExpr]bool {
	pairs := map[*ast.CallExpr]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		b, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i := 0; i+1 < len(b.List); i++ {
			mv := s.rtCall(b.List[i], "MoveValues", 3)
			rv := s.rtCall(b.List[i+1], "RetainValues", 2)
			if mv == nil || rv == nil {
				continue
			}
			if exprText(mv.Args[0]) != exprText(rv.Args[0]) || exprText(mv.Args[2]) != exprText(rv.Args[1]) {
				continue
			}
			pairs[mv] = true
		}
		return true
	})
	return pairs
}

// rtCall returns the call of the runtime function name with n arguments
// that the statement st is, or nil.
func (s *keepScan) rtCall(st ast.Stmt, name string, n int) *ast.CallExpr {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok || len(call.Args) != n {
		return nil
	}
	fn := s.c.staticCallee(call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/rt" || fn.Name() != name {
		return nil
	}
	return call
}

// freeCall handles one call for frees.
func (s *keepScan) freeCall(call *ast.CallExpr, each func(bits uint64, site keepSite), edge func(to ast.Node, j, in int)) {
	site := keepSite{pos: call.Pos(), text: exprText(call)}
	tv := s.c.types[call.Fun]
	if tv.IsType() {
		return
	}
	if tv.IsBuiltin() {
		id, ok := ast.Unparen(call.Fun).(*ast.Ident)
		if !ok || len(call.Args) == 0 {
			return
		}
		switch id.Name {
		case "take":
			if u, ok := ast.Unparen(call.Args[0]).(*ast.UnaryExpr); ok && u.Op == token.AND {
				each(s.through(u.X), site)
			} else {
				o, h := s.addrSets(call.Args[0])
				each(o|h, site)
			}
		case "clear", "delete":
			if elemFreeing(s.c.typeOf(call.Args[0])) {
				o, h := s.addrSets(call.Args[0])
				each(o|h, site)
			}
		}
		return
	}
	prim := s.freePrimitive(call)
	targets, known := s.c.keepTargets(call)
	worst := !known || s.c.bodyless(call)
	s.c.callArgs(call, func(key, val ast.Expr, j int) {
		own, held := s.addrSets(val)
		if lentIface(s.c.typeOf(val)) {
			// An ordinary interface loaded through an input lends what it
			// holds, which the input does not own (section 7.9): what a
			// call frees through it, as the receiver of a method or as an
			// argument, is not the input's memory, as an arena's parent
			// allocator is not the arena's. An input that is itself the
			// interface still frees through it.
			held = 0
		}
		if own|held == 0 {
			return
		}
		if prim(j) || worst && reachesFreeing(s.c.typeOf(val)) {
			each(own|held, site)
		}
		for _, t := range targets {
			for i := 0; i < maxInputs; i++ {
				if (own|held)&bit(i) != 0 {
					edge(t.node, j+t.shift, i)
				}
			}
		}
	})
}

// freePrimitive returns which inputs of call (by callArgs's numbering) the
// call frees through by itself: the pointer of an allocator's Free, the
// receiver of an owner's Drop and of a ring's Pop, Reset and Drop, the
// pointer of the runtime's ReleaseValues, and the source of its
// MoveValues (frees leaves out a MoveValues that copyPairs pairs).
func (s *keepScan) freePrimitive(call *ast.CallExpr) func(j int) bool {
	none := func(int) bool { return false }
	if se := funSelector(call.Fun); se != nil {
		sel := s.c.selections[se]
		if sel != nil && sel.Kind() == types2.MethodVal {
			fun, _ := sel.Obj().(*types2.Func)
			if fun == nil {
				return none
			}
			name := fun.Name()
			recvT := sel.Recv()
			if p := pointerElem(recvT); p != nil {
				recvT = p
			}
			if w, ok := recvT.Underlying().(*types2.Wrapper); ok && w.Kind() == types2.WrapRing {
				switch name {
				case "Pop", "Reset", "Drop":
					return func(j int) bool { return j == 0 }
				}
				return none
			}
			if name == "Drop" && len(call.Args) == 0 && isOwner(recvT) {
				return func(j int) bool { return j == 0 }
			}
			if (name == "Free" || name == "FreePages") && len(call.Args) > 0 {
				if sig, ok := fun.Type().(*types2.Signature); ok && sig.Params().Len() > 0 {
					if b, ok := sig.Params().At(0).Type().Underlying().(*types2.Basic); ok && b.Kind() == types2.UnsafePointer {
						return func(j int) bool { return j == 1 }
					}
				}
			}
			return none
		}
	}
	fn := s.c.staticCallee(call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/rt" {
		return none
	}
	switch fn.Name() {
	case "ReleaseValues":
		return func(j int) bool { return j == 0 }
	case "MoveValues":
		return func(j int) bool { return j == 1 }
	}
	return none
}

// bodyless reports whether call calls a function or concrete method that
// has no body here and no entry in the compiler's contract table (an
// assembly routine, a function the build does not load): nothing is known
// of what it does, so it is taken to free through every argument whose
// type reaches memory that may be freed. A contract entry keeps what the
// table says (the runtime's ReleaseValues and MoveValues are in
// freePrimitive); the methods the compiler makes (a ring's, an owner's
// Drop) have no package; an enum's String takes a number. The scheduler
// hooks the compiler generates (schedHooks) are known by what the backend
// interface requires of them.
func (c *checker) bodyless(call *ast.CallExpr) bool {
	fn := c.staticCallee(call)
	if fn == nil || fn.Pkg() == nil || c.keepDecls[fn] != nil {
		return false
	}
	sig, _ := fn.Type().(*types2.Signature)
	if sig == nil {
		return false
	}
	name := compat.Concat(fn.Pkg().Path(), ".", fn.Name())
	if r := sig.Recv(); r != nil {
		T := r.Type()
		if p := pointerElem(T); p != nil {
			T = p
		}
		if _, ok := T.Underlying().(*types2.Wrapper); ok {
			return false
		}
		n, ok := types2.Unalias(T).(*types2.Named)
		if !ok {
			return false
		}
		name = compat.Concat(fn.Pkg().Path(), ".", n.Obj().Name(), ".", fn.Name())
	}
	if schedHooks[name] {
		return false
	}
	_, ok := contract.Lookup(name)
	return !ok
}

// schedHooks are the runtime's scheduler hooks, whose bodies the compiler
// generates to call the program's backend (lib/pluto/rt/sched.p). The
// backend interface lets only FreeTask free a task: Spawn, Park, Wake and
// Yield make it runnable or switch away from it, Init and InInterrupt
// take no task, and NewTask makes one. So these free nothing of their
// caller's; schedFreeTask stays unknown.
var schedHooks = map[string]bool{
	"pluto/rt.schedNewTask": true, "pluto/rt.schedSpawn": true, "pluto/rt.schedPark": true,
	"pluto/rt.schedWake": true, "pluto/rt.schedYield": true, "pluto/rt.schedInit": true,
	"pluto/rt.schedInInterrupt": true,
}

// reachesFreeing reports whether a value of type T may lead to memory that
// a call given it could free or move an owner out of: an owner or counted
// box, an interface, a type parameter or an unsafe.Pointer, or a pointer,
// slice, map, channel, array, struct, union or wrapper that leads to one.
// A function value leads to nothing of the caller's here.
func reachesFreeing(T types2.Type) bool {
	return reaches(T, true, bounds, map[types2.Type]bool{})
}

// reachesArena reports whether a value of type T may lead to a checked
// arena: one, an interface or type parameter, or a pointer, slice, map,
// channel, array, struct, union or wrapper that leads to one. An
// unsafe.Pointer does not: making an arena of one is outside the checks
// (section 18.12), and the drop functions an arena runs are given its
// allocations that way.
func reachesArena(T types2.Type) bool {
	return reaches(T, false, isCheckedArena, map[types2.Type]bool{})
}

// reaches reports whether a value of type T may lead to one whose type
// leaf accepts, through any interface or type parameter, and through an
// unsafe.Pointer when viaUnsafe is set.
func reaches(T types2.Type, viaUnsafe bool, leaf func(types2.Type) bool, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if leaf(T) {
		return true
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		return viaUnsafe && u.Kind() == types2.UnsafePointer
	case *types2.Interface:
		return true
	case *types2.Pointer:
		return reaches(u.Elem(), viaUnsafe, leaf, seen)
	case *types2.Slice:
		return reaches(u.Elem(), viaUnsafe, leaf, seen)
	case *types2.Array:
		return u.Len() > 0 && reaches(u.Elem(), viaUnsafe, leaf, seen)
	case *types2.Map:
		return reaches(u.Key(), viaUnsafe, leaf, seen) || reaches(u.Elem(), viaUnsafe, leaf, seen)
	case *types2.Chan:
		return reaches(u.Elem(), viaUnsafe, leaf, seen)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if reaches(u.Field(i).Type(), viaUnsafe, leaf, seen) {
				return true
			}
		}
	case *types2.Wrapper:
		return reaches(u.Elem(), viaUnsafe, leaf, seen)
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && reaches(v.Payload(), viaUnsafe, leaf, seen) {
				return true
			}
		}
	}
	return false
}

// calleeFrees returns the statement or call by which a callee of call may
// free memory or move an owner out through its argument key, or false.
func (c *checker) calleeFrees(call *ast.CallExpr, key ast.Expr) (keepSite, bool) {
	s := keepScan{c: c}
	prim := s.freePrimitive(call)
	targets, known := c.keepTargets(call)
	worst := !known || c.bodyless(call)
	var site keepSite
	found := false
	c.callArgs(call, func(k, val ast.Expr, j int) {
		if k != key || found {
			return
		}
		if prim(j) || worst && reachesFreeing(c.typeOf(val)) {
			site, found = keepSite{pos: call.Pos(), text: exprText(call)}, true
			return
		}
		for _, t := range targets {
			fs := c.frees[t.node]
			if j+t.shift < len(fs) && fs[j+t.shift].valid() {
				site, found = fs[j+t.shift], true
				return
			}
		}
	})
	return site, found
}

// A freeArg is a view that a call argument passes to a parameter through
// which the callee may free memory or move an owner out, and the statement
// or call by which it may.
type freeArg struct {
	w    view
	site keepSite
}

// freeArgs returns the views the arguments of call pass to parameters
// through which the callee may free, before the call is walked. A counted
// reference held in a local variable that the call frees through counts as
// a view of all of it.
func (f *fn) freeArgs(call *ast.CallExpr) []freeArg {
	if f.env == nil {
		return nil
	}
	tv := f.c.types[call.Fun]
	if tv.IsType() || tv.IsBuiltin() || f.c.scoped.Tasks[call] != nil || f.isImplicitDrop(call) {
		return nil
	}
	var out []freeArg
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		site, ok := f.c.calleeFrees(call, key)
		if !ok {
			return
		}
		// Through the address of a local owner the callee frees memory the
		// owner owns, and memory of the variables the owner holds views of
		// in its own fields (W{p: &y}, section 18.13), but not of those it
		// was lent through an interface (an allocator, section 7.9).
		own, follow := f.ownerAddressed(val)
		for _, w := range f.viewsTo(val, T) {
			if own != nil && w.of != own && !follow[heldKey(w)] {
				continue
			}
			// A checked arena's allocations end only at its Reset and
			// Release (reclaim, section 18.9) and its drop: a chunk its
			// other methods return to the parent holds none of them.
			if w.of != nil && isCheckedArena(w.of.Type()) {
				continue
			}
			out = compat.Append(out, freeArg{w: w, site: site})
		}
		if id, ok := ast.Unparen(key).(*ast.Ident); ok && isCountedBox(f.c.typeOf(id)) {
			if v := f.varOf(id); v != nil && f.vars[v] {
				out = compat.Append(out, freeArg{w: view{of: v, pos: call.Pos(), text: exprText(id)}, site: site})
			}
		}
	})
	return out
}

// ownerAddressed returns the local owner variable whose address, or the
// address of a part of it, val is, or nil; and the views of other variables
// that part holds outside an interface (by heldKey), which a callee given
// the address may free through.
func (f *fn) ownerAddressed(val ast.Expr) (*types2.Var, map[string]bool) {
	u, ok := ast.Unparen(val).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return nil, nil
	}
	v, path, box, ok := f.placePath(u.X)
	if !ok || box || v == nil || !f.vars[v] || !isOwner(v.Type()) {
		return nil, nil
	}
	follow := map[string]bool{}
	T := f.c.typeOf(u.X)
	for _, w := range f.held(v, path) {
		if w.of != v && !lentAt(T, w.at) {
			follow[heldKey(w)] = true
		}
	}
	return v, follow
}

// heldKey names a view by where it was made and what it views, apart from
// where it is held.
func heldKey(w view) string {
	return compat.Concat(strconv.Itoa(int(w.pos)), "|", w.of.Name(), strconv.Itoa(int(w.of.Pos())))
}

// lentAt reports whether a view held at path in a value of type T is held
// in an interface there (lentIface), or under one: lent, not owned. A
// path the types do not follow is taken as not lent.
func lentAt(T types2.Type, path Path) bool {
	for i := 0; ; i++ {
		if T == nil {
			return false
		}
		if lentIface(T) {
			return true
		}
		if i >= len(path) {
			return false
		}
		step := path[i]
		switch u := T.Underlying().(type) {
		case *types2.Struct:
			if !strings.HasPrefix(step, ".") {
				return false
			}
			k, err := strconv.Atoi(strings.SliceFrom(step, 1))
			if err != nil || k < 0 || k >= u.NumFields() {
				return false
			}
			T = u.Field(k).Type()
		case *types2.Array:
			T = u.Elem()
		default:
			return false
		}
	}
}

// freeViews records that call may have freed memory, or moved an owner
// out, through the views args: the views obtained from their owners that
// look at that part are stale, other than &v itself (section 18.3). A call
// through one counted reference may free through every other reference of
// the same type, so the views obtained through those end too (the compiler
// does not know whether they point to the same object).
func (f *fn) freeViews(call *ast.CallExpr, args []freeArg) {
	if f.env == nil {
		return
	}
	for _, a := range args {
		mv := move{kind: mvFree, pos: call.Pos(), to: f.c.freeHow(call, a.site)}
		f.freedPart(a.w.of, a.w.part, mv)
		if isCountedBox(a.w.of.Type()) {
			var others []*types2.Var
			for u := range f.vars {
				if u != a.w.of && types2.Identical(u.Type(), a.w.of.Type()) {
					others = compat.Append(others, u)
				}
			}
			for _, u := range others {
				f.freedPart(u, nil, mv)
			}
		}
	}
}

// freedPart makes the views obtained from v that look at or into the part
// at path stale, as mv says.
func (f *fn) freedPart(v *types2.Var, path Path, mv move) {
	if v == nil || !f.vars[v] || !bounds(v.Type()) {
		return
	}
	// A view of what v holds on the heap kept where the walk cannot follow
	// (hpin, when the view itself was stored) can no longer be checked:
	// the call may free that memory, so it is E3314 (section 18.3). The
	// end of v's block is not checked so: the drop there releases the same
	// memory (item 237).
	if p := f.env[v].hpin; p != nil && p.stored && mv.kind == mvFree && f.report {
		f.c.report(f.c.heapPinnedDiag(v, p, mv))
	}
	f.staleWhere(v, mv, func(w view) bool { return !w.itself && overlap(w.part, path) })
}

// lentIface reports whether T is an ordinary interface, other than a type
// parameter.
func lentIface(T types2.Type) bool {
	if _, tp := types2.Unalias(T).(*types2.TypeParam); tp || T == nil {
		return false
	}
	return types2.IsInterface(T) && !isOwner(T)
}

// freeHow says how a call may free what a view looks at, for messages:
// "v.Push(1), which may free it or move it out (v.a.Free(...), vec.p:446)".
func (c *checker) freeHow(call *ast.CallExpr, site keepSite) string {
	at := c.fset.Position(site.pos)
	where := compat.Concat("line ", strconv.Itoa(at.Line))
	if at.Filename != c.fset.Position(call.Pos()).Filename {
		where = compat.Concat(strings.SliceFrom(at.Filename, strings.LastIndexByte(at.Filename, '/')+1), ":", strconv.Itoa(at.Line))
	}
	if site.pos == call.Pos() {
		return compat.Concat(exprText(call), ", which may free what it looks at or move it out")
	}
	return compat.Concat(exprText(call), ", which may free what it looks at or move it out (", site.text, ", ", where, ")")
}
