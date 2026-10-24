package eir

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Allocations from a local arena (section 18.9), in EIR's own instructions,
// so that both solvers check them with the rules they have.
//
// alloc.Use(a) makes a the current allocator until the Restore it returns
// is dropped. The window runs from the call to the end of its block, or to
// the end of the function for defer alloc.Use(a).Restore() (the window of
// internal/escape/arena.go). The call stores a in an ObjArenaWindow object;
// within the window, these values also hold what the object holds, so
// that they are views of a local arena:
//
//   - allocations from the current allocator (HeapCurrent and
//     HeapCountedCurrent: new and make without an allocator, strings);
//   - the results of alloc.Default() and rt.DefaultAllocator(), which is
//     how a call that leaves out a defaulted allocator parameter, such as
//     vec.New[T](), gets the arena;
//   - the results of calls whose type keeps its allocator out of sight (a
//     string, a box, an interface such as an error): the summary of the
//     callee cannot show that they hold no memory it allocated from the
//     current allocator, so they are assumed to (section 18.9).
//
// A range-over-func body runs within its loop's windows and reads the
// object through its closure record; a function literal's body runs when
// it is called and has windows of its own only. A window is lexical: past
// an earlier Restore, values are still taken to be views of the arena.
type arenaWindow struct {
	use        *ast.CallExpr
	start, end token.Pos
	owner      *fnb  // the function the call of alloc.Use is built in
	obj        ObjID // owner's ObjArenaWindow object, holding the allocator
	typ        types2.Type
	built      bool
}

// windowHome returns the function whose body holds fb's statements: fb, or
// for a range body the function its outermost loop is in.
func (fb *fnb) windowHome() *fnb {
	h := fb
	for h.rng != nil && h.parent != nil {
		h = h.parent
	}
	return h
}

// arenaWindows returns the windows of the calls of alloc.Use in fb's body.
func (fb *fnb) arenaWindows() []*arenaWindow {
	h := fb.windowHome()
	if !h.windowsDone {
		h.windowsDone = true
		if body, ok := h.bodyNode.(*ast.BlockStmt); ok {
			h.windows = h.b.computeArenaWindows(body)
		}
	}
	return h.windows
}

// computeArenaWindows finds the calls of alloc.Use in body and their
// windows, leaving out the bodies of function literals.
func (b *builder) computeArenaWindows(body *ast.BlockStmt) []*arenaWindow {
	var out []*arenaWindow
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
			if call := b.deferredUse(n.Call); call != nil {
				out = append(out, &arenaWindow{use: call, start: call.End(), end: body.End()})
				return false
			}
		case *ast.CallExpr:
			if b.isUse(n) {
				end := body.End()
				for i := len(stack) - 1; i >= 0; i-- {
					switch stack[i].(type) {
					case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
						end = stack[i].End()
						i = -1
					}
				}
				out = append(out, &arenaWindow{use: n, start: n.End(), end: end})
			}
		}
		stack = append(stack, n)
		return true
	})
	return out
}

// deferredUse returns the alloc.Use call of defer alloc.Use(a).Restore(),
// or nil.
func (b *builder) deferredUse(call *ast.CallExpr) *ast.CallExpr {
	se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	inner, ok := ast.Unparen(se.X).(*ast.CallExpr)
	if !ok || !b.isUse(inner) {
		return nil
	}
	return inner
}

// isUse reports whether call calls pluto/alloc.Use.
func (b *builder) isUse(call *ast.CallExpr) bool {
	fn := b.calledFunc(call)
	return fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "pluto/alloc" && fn.Name() == "Use"
}

// arenaCall applies the rules above to the call e, built as c with results
// res, and returns the results to use.
func (fb *fnb) arenaCall(e *ast.CallExpr, c *Call, res []Reg) []Reg {
	if fb.cur == nil || c == nil {
		return res
	}
	if fb.b.isUse(e) {
		if len(c.Args) > 0 && c.Sig != nil && c.Sig.Params().Len() > 0 {
			fb.noteUse(e, c.Args[0], c.Sig.Params().At(0).Type())
		}
		return res
	}
	out := res
	copied := false
	set := func(i int, r Reg) {
		if !copied {
			out = append([]Reg(nil), res...)
			copied = true
		}
		out[i] = r
	}
	types := resultTypes(c.Sig)
	if views := fb.explicitArenas(c, types); len(views) > 0 {
		for i, r := range out {
			if r != 0 && i < len(types) && isString(types[i]) {
				set(i, fb.union(r, views, types[i]))
			}
		}
	}
	vals := fb.windowValues(fb.windowPos(e.Pos()))
	if len(vals) == 0 {
		return out
	}
	current := fb.isCurrentAllocatorCall(c)
	for i, r := range out {
		if r == 0 || i >= len(types) {
			continue
		}
		if current || hidesAllocator(types[i]) {
			set(i, fb.union(r, vals, types[i]))
		}
	}
	return out
}

// explicitArenas returns, for a static call c with a string result, what
// memory allocated from the allocators it is given is a view of: those of
// its alloc.Allocator parameters, and of the alloc.Allocator field of a
// receiver or parameter that points to a struct with one (a
// strings.Builder or vec.Vec started with Init(&a) or New[T](&a)). Text the
// callee returns may come from them, out of sight in the string (the
// explicit arenas of internal/escape/arena.go).
func (fb *fnb) explicitArenas(c *Call, types []types2.Type) []Reg {
	if c.Kind != CallStatic || c.Sig == nil {
		return nil
	}
	text := false
	for _, T := range types {
		text = text || isString(T)
	}
	if !text {
		return nil
	}
	var params []types2.Type
	if r := c.Sig.Recv(); r != nil {
		params = append(params, r.Type())
	}
	for i := 0; i < c.Sig.Params().Len(); i++ {
		params = append(params, c.Sig.Params().At(i).Type())
	}
	var out []Reg
	obj := NoObj
	view := func(a Reg) {
		if a == 0 {
			return
		}
		if obj == NoObj {
			obj = fb.allocatorMemObj(fb.pos)
		}
		out = append(out, fb.arenaView(a, obj))
	}
	for j, a := range c.Args {
		if j >= len(params) || a == 0 {
			continue
		}
		T := params[j]
		if isAllocatorType(T) {
			view(a)
			continue
		}
		if f, FT, ok := allocatorField(T); ok {
			view(fb.load(fb.field(a, f), FT))
		}
	}
	return out
}

// isAllocatorType reports whether T is pluto/rt.Allocator (alloc.Allocator
// is an alias of it).
func isAllocatorType(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	return ok && n.Obj().Name() == "Allocator" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/rt"
}

// allocatorField returns the index and type of the alloc.Allocator field of
// the struct T points to, if it has one.
func allocatorField(T types2.Type) (int, types2.Type, bool) {
	p, ok := types2.Unalias(T).Underlying().(*types2.Pointer)
	if !ok {
		return 0, nil, false
	}
	st, ok := types2.Unalias(p.Elem()).Underlying().(*types2.Struct)
	if !ok {
		return 0, nil, false
	}
	for i := 0; i < st.NumFields(); i++ {
		if FT := st.Field(i).Type(); isAllocatorType(FT) {
			return i, FT, true
		}
	}
	return 0, nil, false
}

// noteUse records, at the call e of alloc.Use, the allocator a it installs:
// what memory allocated from it is a view of (OpArenaView), stored in its
// window's object.
func (fb *fnb) noteUse(e *ast.CallExpr, a Reg, T types2.Type) {
	for _, w := range fb.arenaWindows() {
		if w.use != e || w.built {
			continue
		}
		w.built, w.owner, w.typ = true, fb, T
		w.obj = fb.newObj(Object{Kind: ObjArenaWindow, Type: T, Name: "arena window", Pos: e.Pos(), Life: LifeFrame})
		fb.store(fb.addr(w.obj), fb.arenaView(a, w.obj), T)
	}
}

// arenaView returns what memory allocated from the allocator a is a view
// of; obj stands for a frame allocator that is not a checked arena variable.
func (fb *fnb) arenaView(a Reg, obj ObjID) Reg {
	if a == 0 {
		return 0
	}
	return fb.emitDst(Instr{Op: OpArenaView, X: a, Obj: obj, Pos: fb.pos})
}

// allocatorMemObj returns a new ObjArenaWindow object for the memory of an
// allocator given at pos that is a frame object but not a checked arena
// variable (OpArenaView).
func (fb *fnb) allocatorMemObj(pos token.Pos) ObjID {
	return fb.newObj(Object{Kind: ObjArenaWindow, Type: types2.Typ[types2.UnsafePointer], Name: "allocator memory", Pos: pos, Life: LifeFrame})
}

// isCheckedArena reports whether T is one of the checked arenas of
// pluto/arena, Fixed and Chunked. The manual arena over a caller's buffer
// (arena.NewUnsafe) is left to the program: its memory is not reclaimed
// when the arena goes (section 18.9).
func isCheckedArena(T types2.Type) bool {
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "pluto/arena" {
		return false
	}
	switch n.Obj().Name() {
	case "Fixed", "Chunked":
		return true
	}
	return false
}

// windowValues returns the allocators installed by the windows open at pos
// in fb: a load of each window's object.
func (fb *fnb) windowValues(pos token.Pos) []Reg {
	var out []Reg
	for _, w := range fb.arenaWindows() {
		if !w.built || pos < w.start || pos >= w.end || !fb.within(w.owner) {
			continue
		}
		if r := fb.load(fb.objAddr(w.owner, w.obj), w.typ); r != 0 {
			out = append(out, r)
		}
	}
	return out
}

// within reports whether fb is owner, or a range body whose loop is in
// owner (directly or through other range bodies).
func (fb *fnb) within(owner *fnb) bool {
	for f := fb; f != nil; f = f.parent {
		if f == owner {
			return true
		}
		if f.rng == nil {
			return false
		}
	}
	return false
}

// inArena returns the value of an allocation from the current allocator,
// r, with the allocators of the windows open here.
func (fb *fnb) inArena(r Reg) Reg {
	if r == 0 || fb.cur == nil {
		return r
	}
	return fb.union(r, fb.windowValues(fb.windowPos(fb.pos)), types2.Typ[types2.UnsafePointer])
}

// windowPos is the position whose windows apply to a value built at pos:
// pos, or the call whose default argument is being built.
func (fb *fnb) windowPos(pos token.Pos) token.Pos {
	if fb.defaultAt.IsValid() {
		return fb.defaultAt
	}
	return pos
}

// union returns a register holding what r and each of vals hold.
func (fb *fnb) union(r Reg, vals []Reg, T types2.Type) Reg {
	if r == 0 || len(vals) == 0 {
		return r
	}
	args := append([]Reg{r}, vals...)
	return fb.emitDst(Instr{Op: OpCompose, Args: args, Fields: make([][]int, len(args)), Type: T, Pos: fb.pos})
}

// isCurrentAllocatorCall reports whether c calls alloc.Default or
// rt.DefaultAllocator, which return the current allocator.
func (fb *fnb) isCurrentAllocatorCall(c *Call) bool {
	if c.Kind != CallStatic || c.Callee == NoFunc || int(c.Callee) >= len(fb.b.p.Funcs) {
		return false
	}
	switch fb.b.p.Funcs[c.Callee].Name {
	case "pluto/alloc.Default", "pluto/rt.DefaultAllocator":
		return true
	}
	return false
}

// releasesCounted reports whether a value of T holds counted references a
// copy of it releases: a string, a shared or synced box, a channel, or a
// type parameter (an instance may be one), directly or in its fields.
func releasesCounted(T types2.Type) bool { return releasesSeen(T, map[types2.Type]bool{}) }

func releasesSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if isTypeParam(T) || isString(T) || isCountedBox(T) {
		return true
	}
	switch u := types2.Unalias(T).Underlying().(type) {
	case *types2.Chan:
		return true
	case *types2.Array:
		return u.Len() > 0 && releasesSeen(u.Elem(), seen)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if releasesSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

// hidesAllocator reports whether a value of T may hold memory whose
// allocator is out of the analysis's sight: a string, a box or an
// interface.
func hidesAllocator(T types2.Type) bool {
	switch u := types2.Unalias(T).Underlying().(type) {
	case *types2.Basic:
		return u.Kind() == types2.String
	case *types2.Interface:
		return true
	case *types2.Wrapper:
		return u.IsBox()
	}
	return false
}
