package escape

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Local arenas installed as the default allocator (language spec section
// 7.1). alloc.Use(&a) makes a the running task's default allocator until
// the Restore it returns is dropped; when a lives in the frame, what is
// allocated through the default allocator in that window lives in a too,
// and must not outlive it. The Restore is already a view of a
// (//pu:resultholds), which keeps it from outliving a; this file makes the
// allocations views of a as well.
//
// The window runs from the call of alloc.Use to the end of the block the
// call is in, or to the end of the function when the Restore is deferred
// (defer alloc.Use(&a).Restore()). Within it, these results are views of
// a:
//
//   - new and make without an allocator argument, string concatenation and
//     the conversions that allocate (string(runes), string(r));
//   - calls that leave out a defaulted allocator parameter, such as
//     vec.New[T]() or alloc.Shared[T](), whose default is alloc.Default();
//   - calls whose result is a string, an owned, shared, synced or weak
//     reference, or an interface: these keep the allocator they came from
//     out of sight (a counted string's buffer, a box's header, an error),
//     so the analysis cannot tell whether they came from a.
//
// What a callee allocates through the default allocator and keeps (stores
// in a package variable or the heap) is not seen here; the arena's epochs
// catch that at run time (design D4, roadmap).

// An arenaWindow is the part of a function in which a local arena is the
// default allocator.
type arenaWindow struct {
	use        *ast.CallExpr
	start, end token.Pos
	l          *loc // lkArena: memory allocated from the arena, once known
}

// computeArenaWindows finds the calls of alloc.Use in a function body.
func (w *walker) computeArenaWindows(body *ast.BlockStmt) {
	if body == nil {
		return
	}
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // its body runs when it is called
		case *ast.DeferStmt:
			if call := w.useCall(n.Call); call != nil {
				w.arenas = append(w.arenas, &arenaWindow{use: call, start: call.End(), end: body.End()})
				return false
			}
		case *ast.CallExpr:
			if w.isUse(n) {
				end := body.End()
				for i := len(stack) - 1; i >= 0; i-- {
					switch stack[i].(type) {
					case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
						end = stack[i].End()
						i = -1
					}
				}
				w.arenas = append(w.arenas, &arenaWindow{use: n, start: n.End(), end: end})
			}
		}
		stack = append(stack, n)
		return true
	})
}

// useCall returns the alloc.Use call in a deferred call on its result,
// defer alloc.Use(&a).Restore(), or nil.
func (w *walker) useCall(call *ast.CallExpr) *ast.CallExpr {
	se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	inner, ok := ast.Unparen(se.X).(*ast.CallExpr)
	if !ok || !w.isUse(inner) {
		return nil
	}
	return inner
}

// isUse reports whether call calls pluto/alloc.Use.
func (w *walker) isUse(call *ast.CallExpr) bool {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	}
	if id == nil {
		return false
	}
	fn, ok := w.objOf(id).(*types2.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "pluto/alloc" && fn.Name() == "Use"
}

// noteArena records, at the call of alloc.Use, what its allocator argument
// points to: if that is a frame, allocations in the window are views of it.
func (w *walker) noteArena(call *ast.CallExpr, arg tset) {
	for _, win := range w.arenas {
		if win.use != call {
			continue
		}
		for _, t := range arg {
			if t.kind != tFrame {
				continue
			}
			if win.l == nil || t.loc.level > win.l.level {
				l := &loc{kind: lkArena, node: call, fn: t.loc.fn, pos: call.Pos(), level: t.loc.level, typ: t.loc.typ}
				if t.loc.isVarLoc() {
					l.v = t.loc.v
				}
				win.l = l
			}
		}
	}
}

// arenaAt returns the tokens of the local arena that is the default
// allocator at pos, or nil.
func (w *walker) arenaAt(pos token.Pos) tset {
	var out tset
	for _, win := range w.arenas {
		if win.l != nil && win.start <= pos && pos < win.end {
			out.add(frameTok(win.l))
		}
	}
	return out
}

// arenaResult adds to what call e's results point to the arena that is the
// default allocator there, for the results that may have been allocated
// from it (see above).
func (w *walker) arenaResult(e *ast.CallExpr, res []tset) []tset {
	if len(w.arenas) == 0 || w.isUse(e) {
		return res
	}
	ar := w.arenaAt(e.Pos())
	if len(ar) == 0 {
		return res
	}
	tv := w.a.types[e.Fun]
	allocates := false
	switch {
	case tv.IsType():
		// string(b), []byte(s) and friends copy into new memory.
		if len(e.Args) == 1 {
			from, to := w.typeOf(e.Args[0]), tv.Type
			allocates = from != nil && isString(to) != isString(from)
		}
	case tv.IsBuiltin():
		// new and make with no allocator argument.
		switch name := builtinName(e); name {
		case "new":
			allocates = len(e.Args) == 1
		case "make":
			allocates = !w.lastIsAllocator(e)
		}
	default:
		allocates = w.omitsAllocator(e)
	}
	types := resultTypes(w.typeOf(e))
	for i := range res {
		if i >= len(types) || !hasPointers(types[i]) {
			continue
		}
		if allocates || hidesAllocator(types[i]) {
			res[i] = union(res[i], ar)
		}
	}
	return res
}

// builtinName returns the name of the predeclared function e calls.
func builtinName(e *ast.CallExpr) string {
	switch f := ast.Unparen(e.Fun).(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// lastIsAllocator reports whether make's last argument is an allocator.
func (w *walker) lastIsAllocator(e *ast.CallExpr) bool {
	if len(e.Args) < 2 {
		return false
	}
	T := w.typeOf(e.Args[len(e.Args)-1])
	return T != nil && isInterface(T)
}

// omitsAllocator reports whether call e leaves out a defaulted parameter of
// type alloc.Allocator, which then takes alloc.Default().
func (w *walker) omitsAllocator(e *ast.CallExpr) bool {
	sig, _ := w.typeOf(e.Fun).Underlying().(*types2.Signature)
	if sig == nil || e.Ellipsis.IsValid() {
		return false
	}
	n := len(e.Args)
	if n == 1 {
		if tup, ok := w.typeOf(e.Args[0]).(*types2.Tuple); ok {
			n = tup.Len()
		}
	}
	for i := n; i < sig.Params().Len(); i++ {
		if isAllocatorType(sig.Params().At(i).Type()) {
			return true
		}
	}
	return false
}

// isAllocatorType reports whether T is pluto/rt.Allocator (alloc.Allocator
// is an alias of it).
func isAllocatorType(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	return ok && n.Obj().Name() == "Allocator" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/rt"
}

// hidesAllocator reports whether a value of T may hold memory whose
// allocator is out of the analysis's sight: a string, a box or an
// interface.
func hidesAllocator(T types2.Type) bool {
	switch u := underlying(T).(type) {
	case *types2.Basic:
		return u.Kind() == types2.String
	case *types2.Interface:
		return true
	case *types2.Wrapper:
		return u.IsBox()
	}
	return false
}

// resultTypes returns the types of a call's results.
func resultTypes(T types2.Type) []types2.Type {
	if tup, ok := T.(*types2.Tuple); ok {
		out := make([]types2.Type, tup.Len())
		for i := range out {
			out[i] = tup.At(i).Type()
		}
		return out
	}
	if T == nil {
		return nil
	}
	return []types2.Type{T}
}
