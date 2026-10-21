package eir

import (
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Deferred calls and task statements.
//
// A defer statement evaluates the function value, receiver and arguments
// where it is, into frame objects of the function whose returns run the
// call (the enclosing function, for a defer in a range-over-func body), and
// the call is made at every return of that function, the last deferred
// first, as internal/lower makes it (stmt.go deferStmt, func.go
// emitReturn).

// deferred is a deferred call: run makes it in the root function.
type deferred struct {
	run func(fb *fnb)
}

// saved is a value a defer statement saved: a frame object of the root.
type saved struct {
	obj ObjID
	T   types2.Type
}

// save stores r, of type T, in a new frame object of the root function,
// from fb, and returns it.
func (fb *fnb) save(r Reg, T types2.Type) saved {
	root := fb.root
	o := root.newObj(Object{Kind: ObjTemp, Type: T, Name: "defer"})
	fb.store(fb.objAddr(root, o), r, T)
	return saved{obj: o, T: T}
}

// loadSaved reads a saved value in the root function.
func (fb *fnb) loadSaved(s saved) Reg { return fb.load(fb.addr(s.obj), s.T) }

func (fb *fnb) saveAll(rs []Reg, types []types2.Type) []saved {
	out := make([]saved, len(rs))
	for i, r := range rs {
		var T types2.Type = types2.Typ[types2.UnsafePointer]
		if i < len(types) && types[i] != nil {
			T = types[i]
		}
		out[i] = fb.save(r, T)
	}
	return out
}

func (fb *fnb) loadAll(ss []saved) []Reg {
	out := make([]Reg, len(ss))
	for i, s := range ss {
		out[i] = fb.loadSaved(s)
	}
	return out
}

// runDefers makes the deferred calls of a return, the last first.
func (fb *fnb) runDefers() {
	if fb.rng != nil {
		return // a loop body's deferred calls are its root's
	}
	for i := len(fb.defers) - 1; i >= 0; i-- {
		fb.ensureBlock()
		fb.defers[i].run(fb)
	}
}

func (fb *fnb) deferStmt(st *ast.DeferStmt) {
	root := fb.root
	call := st.Call
	pos := call.Pos()
	var run func(fb *fnb)
	funTV := fb.b.types[call.Fun]
	fun := ast.Unparen(call.Fun)
	switch {
	case funTV.IsBuiltin():
		name := fb.b.uses[identOf(fun)].Name()
		switch name {
		case "print", "println", "close", "panic":
			for _, a := range call.Args {
				fb.expr(a)
			}
			run = func(*fnb) {}
		case "clear":
			x := fb.expr(call.Args[0])
			xs := fb.save(x, fb.typeOf(call.Args[0]))
			T := fb.typeOf(call.Args[0])
			run = func(fb *fnb) {
				fb.clear(fb.loadSaved(xs), T, pos, ImplDefer)
			}
		case "copy":
			d := fb.save(fb.expr(call.Args[0]), fb.typeOf(call.Args[0]))
			s := fb.save(fb.expr(call.Args[1]), fb.typeOf(call.Args[1]))
			et := sliceElem(fb.typeOf(call.Args[0]))
			run = func(fb *fnb) {
				fb.copyElems(fb.loadSaved(d), fb.loadSaved(s), et, pos)
			}
		case "delete":
			mT := fb.typeOf(call.Args[0])
			m := mapOf(mT)
			mv := fb.save(fb.expr(call.Args[0]), mT)
			kv := fb.save(fb.exprAs(call.Args[1], m.Key()), m.Key())
			run = func(fb *fnb) {
				fb.mapCall("mapDelete", m, []Reg{fb.loadSaved(mv), fb.loadSaved(kv)}, pos, ImplDefer)
			}
		default:
			// The lowerer rejects it; its arguments are built.
			for _, a := range call.Args {
				fb.expr(a)
			}
			run = func(*fnb) {}
		}
	case fb.b.moves.DeferredDrops[call] || fb.implicitDropCall(call):
		// defer x.Drop() on an owner variable: x's whole drop glue runs
		// when the function returns.
		x := ast.Unparen(call.Fun).(*ast.SelectorExpr).X
		lv := fb.lvalue(x)
		T := unwrap(lv.T)
		a := fb.save(fb.lvAddr(lv), types2.NewPointer(T))
		run = func(fb *fnb) {
			fb.dropAt(fb.loadSaved(a), T, pos)
		}
	default:
		c, args, types := fb.calleeAndArgs(call, true)
		if c == nil {
			return
		}
		if c.Kind == CallContract && fb.b.contractInline(c.Contract) {
			// An inline expansion: nothing is called at exit.
			run = func(*fnb) {}
			break
		}
		var target saved
		hasTarget := c.Kind == CallFunc || c.Kind == CallIface
		if hasTarget {
			target = fb.save(c.Target, fb.targetType(c))
		}
		sargs := fb.saveAll(args, types)
		proto := *c
		run = func(fb *fnb) {
			c := proto
			if hasTarget {
				c.Target = fb.loadSaved(target)
			}
			c.Args = fb.loadAll(sargs)
			c.Implicit = ImplDefer
			fb.emitCall(&c, pos)
		}
	}
	root.defers = append(root.defers, &deferred{run: run})
}

// targetType is the type a dynamic call's target is saved as.
func (fb *fnb) targetType(c *Call) types2.Type {
	if c.Kind == CallIface && c.Iface != nil {
		return c.Iface
	}
	if c.Sig != nil {
		return c.Sig
	}
	return types2.Typ[types2.UnsafePointer]
}

// implicitDropCall reports whether call is x.Drop() on a variable x whose
// owner type declares no Drop.
func (fb *fnb) implicitDropCall(call *ast.CallExpr) bool {
	se, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	if _, ok := ast.Unparen(se.X).(*ast.Ident); !ok {
		return false
	}
	sel := fb.b.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return false
	}
	fn, _ := sel.Obj().(*types2.Func)
	return fn != nil && isImplicitDrop(fn)
}

// isImplicitDrop reports whether fn is the Drop that x.Drop() selects on an
// owner whose type declares none (section 18.6).
func isImplicitDrop(fn *types2.Func) bool {
	return fn.Name() == "Drop" && fn.Pkg() == nil && types2.BoxMethodOf(fn) == nil
}

// identOf returns the identifier of a name or qualified name.
func identOf(e ast.Expr) *ast.Ident {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

// taskStmt builds a task statement (section 12.12): the function value,
// receiver and arguments are evaluated here and handed to the new task,
// which outlives the frame (a task sink), and the task calls the function
// with them.
func (fb *fnb) taskStmt(st *ast.TaskStmt) {
	call := st.Call
	if fb.b.types[call.Fun].IsBuiltin() {
		for _, a := range call.Args {
			fb.expr(a)
		}
		return
	}
	c, args, _ := fb.calleeAndArgs(call, true)
	if c == nil {
		return
	}
	if c.Kind == CallFunc || c.Kind == CallIface {
		fb.sink(SinkTask, c.Target, 0, call.Pos())
	}
	for _, a := range args {
		fb.sink(SinkTask, a, 0, call.Pos())
	}
	c.Args = args
	c.Implicit = ImplTask
	fb.emitCall(c, call.Pos())
}

// scopedTask builds s.Task(lit) on a local sync.Scope (section 19.4): the
// literal runs as a task that the Scope waits for before its block ends,
// so it may use the frame; the call is a call of the literal. The task runs
// until the Scope's block ends, not only until the call returns: what its
// record carries (the address of a variable it shares, the value of one it
// takes) is stored into the Scope variable, which keeps the task, so that
// a view the task keeps past the iteration of a loop that made it is the
// store check's E3006.
func (fb *fnb) scopedTask(e *ast.CallExpr, t *scoped.Task) {
	lit := t.Lit
	clo, id := fb.funcLit(lit)
	if a := fb.varAddr(t.Scope.Var); a != 0 {
		if lf := fb.b.litFns[lit]; lf != nil {
			ptr := types2.Typ[types2.UnsafePointer]
			for _, k := range lf.caps {
				addr := fb.outerAddr(k)
				if v, ok := k.(*types2.Var); ok && t.Taken[v] {
					fb.store(a, fb.load(addr, v.Type()), v.Type())
					continue
				}
				fb.store(a, addr, ptr)
			}
		}
	}
	fb.emitCall(&Call{Kind: CallStatic, Callee: id, Sig: fb.b.types[lit].Type.(*types2.Signature), Args: []Reg{clo}, Implicit: ImplTask}, e.Pos())
}
