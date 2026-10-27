// Translated by go2pu from internal/escape2/report/text.go at the go-cutover tag; edited by hand since.

package report

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2/solve"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
	"pluto/unicode/utf8"
)

// Messages. Every escape is reported as a view of the frame (a pointer, a
// slice, a closure, an interface's copy) reaching a place that outlives
// what it looks at. The message names the expression, what it looks at and
// where that lives, says why the place outlives it, and shows fixes as code
// (language spec section 21.1). The text is that of internal/escape, so
// that a program gets the same message from either analysis, except where
// that text was wrong.

// A sinkKind says where a view of the frame went.
type sinkKind uint8

const (
	sinkReturn     sinkKind = iota // returned
	sinkStore                      // stored in memory that outlives the frame
	sinkCall                       // passed to a function that keeps it
	sinkGo                         // passed to a task statement
	sinkSend                       // sent on a channel
	sinkLoop                       // kept in a variable the next iteration of a loop can see
	sinkRegionLoop                 // the closure of a call in a loop kept past the iteration (E3101)
	sinkBox                        // converted to an interface, whose copy stays in the frame until the function returns (E3313)
)

// A storeKind says why the memory a view is stored in outlives it.
type storeKind uint8

const (
	storeHeap     storeKind = iota // memory that may be on the heap
	storeGlobal                    // a package variable
	storeParam                     // memory reached through a parameter
	storeOuter                     // the frame of the function around a literal
	storeStmt                      // anything that outlives the statement making an owner temporary
	storeLoopBody                  // what the function around the body of a range loop over a function keeps
)

// A viewErr is an escape, with what a message needs to explain it.
type viewErr struct {
	sink sinkKind
	pos  token.Pos
	expr ast.Expr // the expression holding the view, if known
	l    *loc     // what it looks at

	fn string // the function the view leaves, or whose frame it is in

	// sinkStore and sinkLoop: where it is stored.
	dst   string
	where storeKind
	inMap string // "key" or "element" of a map, or ""
	via   string // storeParam: the parameter the memory is reached through
	viaFn string // storeParam: the function whose parameter it is, if not fn

	// sinkCall, and sinkLoop when a callee keeps the view: the function
	// called and what it does with the view ("stores p in G (a.p:3)").
	callee    string
	calleeWhy string

	call *ast.CallExpr // sinkGo: the call of the task statement
	ch   ast.Expr      // sinkSend: the channel

	loop    ast.Node    // sinkLoop: the loop whose iterations reuse l
	iterVar *types2.Var // sinkLoop with a closure: the loop's variable it uses

	exprType types2.Type     // the type of expr, if known
	qual     *types2.Package // the package the messages are relative to
}

// upperFirst capitalizes the first letter of s.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	_, n := utf8.DecodeRuneInString(s)
	return compat.Concat(strings.ToUpper(strings.Slice(s, 0, n)), strings.SliceFrom(s, n))
}

// tpargText names the type argument a type-parameter box is copied for:
// "P", or "a type larger than one word" when the program gives none.
func (r *reporter) tpargText(l *loc, q types2.Qualifier) string {
	if l.tparg != nil {
		return typeText(l.tparg, q)
	}
	return "a type larger than one word"
}

// tpargUse says which type argument of the program is copied: " The
// program uses f with T = P, which is larger than one word."
func (r *reporter) tpargUse(l *loc, q types2.Qualifier) string {
	if l.tparg == nil {
		return ""
	}
	return compat.Sprintfb(" The program uses %s with %s = %s, which is larger than one word.", compat.ArgS(l.fn), compat.ArgS(l.tparam.Obj().Name()), compat.ArgS(typeText(l.tparg, q)))
}

// sentence joins the non-empty sentences of a paragraph.
func sentence(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = compat.Append(out, p)
		}
	}
	return compat.Join(out, " ")
}

// addrOfVar reports whether e is &v, &v.f or &v[i] for l's variable.
func addrOfVar(e ast.Expr, l *loc) bool {
	u, ok := ast.Unparen(e).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND || !l.isVarLoc() {
		return false
	}
	return rootIdent(u.X) == l.v.Name()
}

// sliceOfVar reports whether e is v[i:j] for l's array variable.
func sliceOfVar(e ast.Expr, l *loc) bool {
	s, ok := ast.Unparen(e).(*ast.SliceExpr)
	if !ok || !l.isVarLoc() {
		return false
	}
	return rootIdent(s.X) == l.v.Name()
}

func rootIdent(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// is reports whether e makes l itself (a literal, a closure, a box).
func is(e ast.Expr, l *loc) bool {
	if e == nil || l.node == nil {
		return false
	}
	n := ast.Unparen(e)
	if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
		n = ast.Unparen(u.X)
	}
	return n == l.node
}

// viewPhrase says what x is in terms of l, for a first line: ", a pointer
// to a local variable", ", which points into x, a local variable of f".
func (r *reporter) viewPhrase(e ast.Expr, l *loc, q types2.Qualifier) string {
	switch {
	case addrOfVar(e, l):
		return compat.Concat(", a pointer to ", l.kindWord())
	case sliceOfVar(e, l):
		return compat.Concat(", a slice of ", l.v.Name(), ", ", l.kindWord())
	case l.kind == lkClosure && is(e, l):
		return compat.Concat(": it uses ", l.captureList(), ", ", l.captureNoun())
	case l.kind == lkLit && is(e, l):
		return compat.Concat(", a pointer to a composite literal in ", possessive(l.fn), " frame")
	case l.kind == lkSliceLit && is(e, l):
		return compat.Concat(", a slice literal whose elements are in ", possessive(l.fn), " frame")
	case l.kind == lkMethodValue && is(e, l):
		return compat.Concat(", a method value whose receiver is in ", possessive(l.fn), " frame")
	case l.kind == lkBox:
		return compat.Concat(", an interface holding a copy of ", article(typeText(l.typ, q)), " in ", possessive(l.fn), " frame")
	case l.kind == lkClosure:
		return compat.Concat(", a function literal that uses ", l.captureList(), ", ", l.captureNoun())
	}
	return compat.Concat(", which points into ", r.noun(l, q))
}

// subject is x as the subject of a sentence about where it points.
func (r *reporter) subject(x string, e ast.Expr, l *loc, q types2.Qualifier) string {
	switch {
	case addrOfVar(e, l):
		return compat.Concat(x, " is a pointer to ", l.v.Name(), ".")
	case sliceOfVar(e, l):
		return compat.Concat(x, " is a slice that looks at ", l.v.Name(), ".")
	case is(e, l):
		return ""
	case l.isVarLoc():
		return compat.Concat(x, " points into ", l.v.Name(), ".")
	case l.kind == lkClosure:
		return compat.Concat(x, " is a function literal that uses ", l.captureList(), ".")
	case l.kind == lkLoopBody:
		return ""
	case l.kind == lkObject && l.v != nil && l.ref != "" && x == l.v.Name():
		return ""
	case l.kind == lkBox:
		return compat.Concat(x, " is an interface that points at a copy of its value in ", possessive(l.fn), " frame.")
	}
	return compat.Concat(x, " points into ", r.noun(l, q), ".")
}

// defaultText is how a message writes a view of l when it has no
// expression for it.
func defaultText(l *loc) string {
	switch l.kind {
	case lkVar, lkParam, lkResult:
		return compat.Concat("&", l.v.Name())
	case lkClosure:
		return "the function literal"
	case lkLoopBody:
		return "the loop body"
	case lkVarargs:
		if call, ok := l.node.(*ast.CallExpr); ok {
			return compat.Concat("the variadic arguments of ", exprText(call.Fun))
		}
	}
	if e, ok := l.node.(ast.Expr); ok && e != nil {
		return exprText(e)
	}
	return compat.Concat("a pointer into ", possessive(l.fn), " frame")
}

func varType(l *loc, q types2.Qualifier) string {
	if l.v != nil {
		return typeText(l.v.Type(), q)
	}
	return typeText(l.typ, q)
}

// isInterface reports whether T is an interface type; a type parameter,
// whose underlying type is its constraint, is not.
func isInterface(T types2.Type) bool {
	if T == nil {
		return false
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return false
	}
	_, ok := T.Underlying().(*types2.Interface)
	return ok
}

// viewDiag builds the diagnostic for e.
func (r *reporter) viewDiag(e *viewErr) *diag.Diagnostic {
	l, q := e.l, types2.RelativeTo(e.qual)
	x := exprText(e.expr)
	if x == "" {
		x = defaultText(l)
	}
	d := pu.New(diag.Diagnostic{Pos: r.fset.Position(e.pos)})
	var text []string
	add := func(s string) {
		if s != "" {
			text = compat.Append(text, s)
		}
	}
	reclaimed := compat.Sprintfb("When %s returns, %s memory is reclaimed", compat.ArgS(l.fn), compat.ArgS(possessive(l.name())))
	switch {
	case l.kind == lkArena:
		reclaimed = compat.Sprintfb("When %s returns, the arena %s is gone", compat.ArgS(l.fn), compat.ArgS(l.name()))
	case l.kind == lkObject && l.v != nil && l.ref != "":
		reclaimed = compat.Sprintfb("When %s returns, %s drops its reference, and the %s is freed if that was the last one", compat.ArgS(l.fn), compat.ArgS(l.v.Name()), compat.ArgS(l.ref))
	case l.kind == lkObject && l.v != nil:
		reclaimed = compat.Sprintfb("When %s returns, %s is dropped and the object freed", compat.ArgS(l.fn), compat.ArgS(l.v.Name()))
	case l.kind == lkObject:
		reclaimed = "At the end of the statement, the temporary is dropped and its object freed"
	case l.kind == lkStmtTemp:
		reclaimed = "At the end of the statement, the temporary is dropped"
	case !l.isVarLoc() && l.kind != lkClosure:
		reclaimed = compat.Sprintfb("When %s returns, its frame is reclaimed", compat.ArgS(l.fn))
	}
	pointer := x
	if l.kind == lkBox {
		pointer = "the interface"
	}
	about := sentence(r.subject(x, e.expr, l, q), r.intro(l, q))

	switch e.sink {
	case sinkReturn:
		d.Code = diag.ReturnFrameView
		d.Msg = compat.Concat("cannot return ", x, r.viewPhrase(e.expr, l, q))
		converted := e.expr != nil && e.exprType != nil && !isInterface(e.exprType)
		lends := l.kind == lkObject && converted && (isBoxPtr(e.exprType) || refNoun(e.exprType) != "")
		switch {
		case l.kind == lkBox && converted && l.tparam != nil:
			// The value is copied for some type arguments only: say for
			// which. (internal/escape said "would hold a copy" whatever the
			// type argument, which is wrong for a pointer such as *int: the
			// interface then holds the pointer itself.)
			d.Msg = compat.Sprintfb("cannot return %s as an interface: when %s is %s, the interface holds a copy of it in %s frame", compat.ArgS(x), compat.ArgS(l.tparam.Obj().Name()), compat.ArgS(r.tpargText(l, q)), compat.ArgS(possessive(l.fn)))
		case l.kind == lkBox && converted:
			d.Msg = compat.Sprintfb("cannot return %s as an interface: the interface would hold a copy of it in %s frame", compat.ArgS(x), compat.ArgS(possessive(l.fn)))
		case lends && refNoun(e.exprType) != "":
			d.Msg = compat.Sprintfb("cannot return %s as an interface: an ordinary interface only borrows the %s, whose reference %s drops when it returns", compat.ArgS(x), compat.ArgS(refNoun(e.exprType)), compat.ArgS(e.fn))
		case lends:
			d.Msg = compat.Sprintfb("cannot return %s as an interface: an ordinary interface only borrows the box, which is dropped when %s returns", compat.ArgS(x), compat.ArgS(e.fn))
		}
		switch {
		case l.kind == lkArena:
			add(sentence(about, compat.Sprintfb("%s, so %s would point at memory that is no longer allocated.", compat.ArgS(reclaimed), compat.ArgS(x))))
		case lends && refNoun(e.exprType) == "map":
			add(sentence(about, compat.Sprintfb("Converting %s to an ordinary interface lends it: the interface points at the map's table without a reference of its own.", compat.ArgS(x))))
		case lends && refNoun(e.exprType) == "channel":
			add(sentence(about, compat.Sprintfb("Converting %s to an ordinary interface lends it: the interface points at the channel without a reference of its own.", compat.ArgS(x))))
		case lends:
			add(sentence(about, compat.Sprintfb("Converting %s to an ordinary interface lends it: the interface points at the object, and %s still owns it.", compat.ArgS(x), compat.ArgS(x))))
		case l.kind == lkObject:
			add(sentence(about, compat.Sprintfb("%s, so %s would point at freed memory.", compat.ArgS(reclaimed), compat.ArgS(x))))
		case l.kind == lkStmtTemp:
			add(sentence(about, compat.Sprintfb("%s, before the caller gets %s, so it would point at a value that has been dropped.", compat.ArgS(reclaimed), compat.ArgS(x))))
		case l.isVarLoc():
			add(sentence(about, compat.Sprintfb("Its memory is reclaimed when %s returns, so %s would point at memory that no longer holds %s.", compat.ArgS(e.fn), compat.ArgS(x), compat.ArgS(l.v.Name()))))
		case l.kind == lkClosure:
			add(sentence(about, compat.Sprintfb("That frame is reclaimed when %s returns.", compat.ArgS(e.fn))))
		default:
			add(sentence(about, compat.Sprintfb("That frame is reclaimed when %s returns, so %s would point at memory that no longer holds the value.", compat.ArgS(e.fn), compat.ArgS(pointer))))
		}
		r.returnFixes(d, e, l, q)
	case sinkStore:
		d.Code = diag.StoreFrameView
		d.Msg = compat.Sprintfb("cannot store %s in %s, which outlives %s", compat.ArgS(x), compat.ArgS(e.dst), compat.ArgS(l.name()))
		var why string
		switch e.where {
		case storeGlobal:
			why = compat.Sprintfb("%s, but package variables live for the whole program, so %s would be left pointing at reclaimed memory.", compat.ArgS(reclaimed), compat.ArgS(e.dst))
		case storeParam:
			owner := e.fn
			if e.viaFn != "" {
				owner = e.viaFn
			}
			why = compat.Sprintfb("%s, but %s is reached through %s, which points to memory that %s caller owns and that lives longer. It would be left pointing at reclaimed memory.", compat.ArgS(reclaimed), compat.ArgS(e.dst), compat.ArgS(e.via), compat.ArgS(possessive(owner)))
		case storeHeap:
			why = compat.Sprintfb("%s, but %s may be on the heap and live longer, so it would be left pointing at reclaimed memory.", compat.ArgS(reclaimed), compat.ArgS(e.dst))
			if e.inMap != "" {
				why = compat.Sprintfb("%s, but a map keeps its keys and elements in a table on the heap, which every copy of the map shares and which may live longer, so it would be left pointing at reclaimed memory.", compat.ArgS(reclaimed))
			}
		case storeOuter:
			why = compat.Sprintfb("The function literal's frame is reclaimed each time the literal returns, but %s belongs to the function around it and lives longer.", compat.ArgS(e.dst))
		case storeStmt:
			why = compat.Sprintfb("%s, but %s lives longer, so it would be left pointing at a value that has been dropped.", compat.ArgS(reclaimed), compat.ArgS(e.dst))
		case storeLoopBody:
			why = compat.Sprintfb("The body of a range loop over a function is a function of its own, called once for each iteration (section 12.11), and the variables it declares live only while that call runs. %s outlive it, so they would be left pointing at reclaimed memory.", compat.ArgS(upperFirst(e.dst)))
		}
		add(sentence(about, why))
		r.storeFixes(d, e, l, q)
	case sinkCall:
		d.Code = diag.PassFrameView
		d.Msg = compat.Sprintfb("cannot pass %s to %s, which keeps it after returning", compat.ArgS(x), compat.ArgS(e.callee))
		add(sentence(about, compat.Sprintfb("%s %s, so %s would outlive %s.", compat.ArgS(e.callee), compat.ArgS(e.calleeWhy), compat.ArgS(pointer), compat.ArgS(l.name()))))
		r.passFixes(d, e, l, q)
	case sinkGo:
		d.Code = diag.GoFrameView
		if e.callee != "" {
			d.Msg = compat.Sprintfb("cannot pass %s to %s, which starts a task with it: the new task may outlive %s", compat.ArgS(x), compat.ArgS(e.callee), compat.ArgS(l.name()))
		} else {
			d.Msg = compat.Sprintfb("cannot pass %s to a task statement: the new task may outlive %s", compat.ArgS(x), compat.ArgS(l.name()))
		}
		add(sentence(about, compat.Sprintfb("The task started here runs on its own, and may still be running after %s returns and %s memory is reclaimed.", compat.ArgS(e.fn), compat.ArgS(possessive(l.name())))))
		if addrOfVar(e.expr, l) && solve.IsWaitGroupType(l.v.Type()) {
			// A WaitGroup cannot be copied; section 12.12 names the fixes.
			d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Declare %s at package level, so that it outlives every task", compat.ArgS(l.v.Name())),
				Code: compat.Sprintfb("var %s sync.WaitGroup // at package level", compat.ArgS(l.v.Name()))})
			d.More = compat.Append(d.More, pu.New(diag.Fix{Intro: "Or start the tasks with a sync.Scope, which waits for them before its block ends, so that they may use the function's variables",
				Code: "var s sync.Scope\ns.Task(func() { ... })"}))
			break
		}
		handOffFixes(d, e, l, "task")
		if e.call != nil {
			d.More = compat.Append(d.More, pu.New(diag.Fix{Intro: compat.Sprintfb("Or start it with a sync.Scope, which waits for its tasks before its block ends, so that they may use %s variables", compat.ArgS(possessive(e.fn))),
				Code: compat.Sprintfb("var s sync.Scope\ns.Task(func() { %s })", compat.ArgS(exprText(e.call)))}))
		}
	case sinkSend:
		d.Code = diag.SendFrameView
		d.Msg = compat.Sprintfb("cannot send %s on %s: the receiver may outlive %s", compat.ArgS(x), compat.ArgS(exprText(e.ch)), compat.ArgS(l.name()))
		add(sentence(about, compat.Sprintfb("The value sent here may be received after %s returns and %s memory is reclaimed.", compat.ArgS(e.fn), compat.ArgS(possessive(l.name())))))
		handOffFixes(d, e, l, "send")
	case sinkLoop:
		d.Code = diag.LoopFrameView
		r.loopDiag(d, e, x, add)
	case sinkRegionLoop:
		d.Code = diag.ClosureRegionLoop
		r.regionLoopDiag(d, e, add)
	case sinkBox:
		d.Code = diag.StaleView
		d.Msg = compat.Sprintfb("cannot convert %s to an interface: it points into %s, which is gone before the interface's copy is released", compat.ArgS(x), compat.ArgS(r.noun(l, q)))
		add(sentence(about, compat.Sprintfb("Converting %s to an interface puts a copy of it in %s frame, and the copy holds a reference to the text until %s returns. That is after the arena %s is dropped, and releasing the reference would write into the arena's memory.", compat.ArgS(x), compat.ArgS(possessive(e.fn)), compat.ArgS(e.fn), compat.ArgS(l.name()))))
		d.Fix = pu.New(diag.Fix{
			Intro: "Use the text without converting it to an interface, for example by writing its bytes",
			Code:  "os.Stdout.Write(b.Bytes())",
		})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or convert a string made from an allocator that outlives the arena, passed explicitly, or from the default allocator before the arena is installed"})})
	}
	d.Text = compat.Join(text, "\n")
	if l.kind == lkStmtTemp && e.sink != sinkLoop && e.sink != sinkBox {
		d.Code = diag.TempViewKept
	}
	return d
}

func (r *reporter) returnFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
	switch {
	case l.kind == lkClosure:
		var fields []string
		for _, v := range l.captures {
			fields = compat.Append(fields, compat.Concat(v.Name(), " ", typeText(v.Type(), q)))
		}
		use := "..."
		if len(l.captures) > 0 {
			use = l.captures[0].Name()
		}
		d.Fix = pu.New(diag.Fix{
			Intro: "Return a value that carries its own state instead, such as a struct with a method",
			Code:  compat.Sprintfb("type state struct{ %s }\n\nfunc (s *state) Call() { ... } // uses s.%s", compat.ArgS(compat.Join(fields, "; ")), compat.ArgS(use)),
		})
	case l.kind == lkMethodValue:
		d.Fix = pu.New(diag.Fix{Intro: "Return the receiver itself, and let the caller call the method on it."})
	case l.kind == lkBox && l.tparam != nil:
		name := l.tparam.Obj().Name()
		d.Fix = pu.New(diag.Fix{
			Intro: compat.Sprintfb("Return the value as a %s, changing the result type to %s, and let the caller convert it to an interface where it is used", compat.ArgS(name), compat.ArgS(name)),
			Code:  compat.Concat("return ", exprText(e.expr)),
		})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or call %s only with type arguments that fit in one word, such as pointers.", compat.ArgS(e.fn))})})
	case l.kind == lkBox:
		d.Fix = pu.New(diag.Fix{
			Intro: compat.Sprintfb("Return a pointer to memory that outlives %s, such as a field of a longer-lived value", compat.ArgS(e.fn)),
			Code:  compat.Sprintfb("p.err = %s{...}\nreturn &p.err", compat.ArgS(typeText(l.typ, q))),
		})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "or return a value that fits in one word, such as an error code."})})
	case l.kind == lkObject && e.expr != nil && e.exprType != nil && !isInterface(e.exprType) && refNoun(e.exprType) != "":
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the %s itself, changing the result type to %s: the caller gets a reference of its own", compat.ArgS(refNoun(e.exprType)), compat.ArgS(typeText(e.exprType, q))), Code: compat.Concat("return ", exprText(e.expr))})
	case l.kind == lkObject && e.expr != nil && e.exprType != nil && !isInterface(e.exprType) && isBoxPtr(e.exprType):
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the box itself, changing the result type to %s", compat.ArgS(typeText(e.exprType, q))), Code: compat.Concat("return ", exprText(e.expr))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or declare the interface as an owning interface, which takes ownership of what is converted into it (language spec section 18.5)", Code: "type I owner interface {\n\t...\n}"})})
	case l.kind == lkObject && l.v != nil:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return %s itself: an owned box moves to the caller, and a shared or synced one gives the caller its own reference", compat.ArgS(l.v.Name())), Code: compat.Concat("return ", l.v.Name())})
	case l.kind == lkObject:
		code := "return ..."
		if n, ok := l.node.(ast.Expr); ok {
			code = compat.Concat("return ", exprText(n))
		}
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the box itself, changing the result type to %s", compat.ArgS(typeText(l.typ, q))), Code: code})
	case l.kind == lkLit:
		T := typeText(l.typ, q)
		d.Fix = pu.New(diag.Fix{Intro: "To give the value to the caller, return it instead. This copies it, or moves it if it is an owner", Code: compat.Sprintfb("return %s{...}", compat.ArgS(T))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("To keep it at a fixed address after %s returns, allocate it and return its owner, an owned[%s]", compat.ArgS(e.fn), compat.ArgS(T)), Code: compat.Sprintfb("p := new(%s)\n...\nreturn p", compat.ArgS(T))})})
	case l.kind == lkSliceLit || (sliceOfVar(e.expr, l) && isArrayVar(l.v)):
		d.Fix = pu.New(diag.Fix{Intro: "To give the caller the elements, return an owner of them, such as a vec.Vec", Code: "v := vec.New[T]()\nv.Push(...)\nreturn v"})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "or copy them into memory the caller passes in", Code: "func fill(dst []T) int {\n\t...\n\treturn copy(dst, buf[:n])\n}"})})
	case l.isVarLoc() && addrOfVar(e.expr, l):
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: "To give the value to the caller, return it instead. This copies it, or moves it if it is an owner", Code: compat.Concat("return ", name)})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("To keep it at a fixed address after %s returns, allocate it and return its owner, an owned[%s]", compat.ArgS(e.fn), compat.ArgS(varType(l, q))), Code: compat.Sprintfb("%s := new(%s)\n...\nreturn %s", compat.ArgS(name), compat.ArgS(varType(l, q)), compat.ArgS(name))})})
	case l.kind == lkArena:
		arenaFixes(d)
	case l.kind == lkStmtTemp:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the %s itself, which keeps its memory alive in the caller, or a copy of what the view looks at.", compat.ArgS(typeText(l.typ, q)))})
	case l.isVarLoc():
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return values instead of views of %s, or allocate %s and return its owner, an owned[%s]", compat.ArgS(name), compat.ArgS(name), compat.ArgS(varType(l, q))), Code: compat.Sprintfb("%s := new(%s)\n...\nreturn %s", compat.ArgS(name), compat.ArgS(varType(l, q)), compat.ArgS(name))})
	default:
		d.Fix = pu.New(diag.Fix{Intro: "Return the value itself, which copies it, or allocate it so that it outlives the function."})
	}
}

func (r *reporter) storeFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
	dst := strings.TrimPrefix(e.dst, "package variable ")
	switch {
	case e.inMap != "":
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store the value itself instead of a view of %s, changing the map's %s type to match.", compat.ArgS(l.name()), compat.ArgS(e.inMap))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or keep what the map refers to in memory that lives as long as the map, such as a package variable, or in a counted reference (shared[T] or synced[T]) stored in the map."})})
	case l.kind == lkClosure:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store a value that carries its own state instead of the function literal, such as a struct with a method, or make %s a package variable if it must outlive %s.", compat.ArgS(l.captureList()), compat.ArgS(l.fn))})
	case l.isVarLoc() && addrOfVar(e.expr, l) && !strings.Contains(dst, " "):
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store the value instead of a pointer to it, changing the type of %s to match. This copies %s, or moves it if it is an owner", compat.ArgS(dst), compat.ArgS(name)), Code: compat.Sprintfb("%s = %s", compat.ArgS(dst), compat.ArgS(name))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or allocate %s and store its owner, an owned[%s], which moves into %s", compat.ArgS(name), compat.ArgS(varType(l, q)), compat.ArgS(dst)), Code: compat.Sprintfb("%s = new(%s)", compat.ArgS(dst), compat.ArgS(varType(l, q)))})})
	case l.kind == lkArena:
		arenaFixes(d)
	case l.kind == lkStmtTemp:
		d.Fix = stmtTempFix(l)
	case l.kind == lkObject && l.v != nil && l.ref != "":
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store %s itself, changing the type of %s to %s: it then counts a reference of its own", compat.ArgS(name), compat.ArgS(dst), compat.ArgS(typeText(l.typ, q))), Code: compat.Sprintfb("%s = %s", compat.ArgS(dst), compat.ArgS(name))})
	case l.kind == lkObject && l.v != nil:
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store %s itself, changing the type of %s to match: an owned box moves there, and a shared or synced one is counted", compat.ArgS(name), compat.ArgS(dst)), Code: compat.Sprintfb("%s = %s", compat.ArgS(dst), compat.ArgS(name))})
	case l.isVarLoc():
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store values instead of views of %s, or allocate %s and store its owner, an owned[%s], which moves there", compat.ArgS(name), compat.ArgS(name), compat.ArgS(varType(l, q))), Code: compat.Sprintfb("%s = new(%s)", compat.ArgS(dst), compat.ArgS(varType(l, q)))})
	default:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store the value itself instead of a view of %s frame, or allocate it so that it lives as long as %s.", compat.ArgS(possessive(l.fn)), compat.ArgS(e.dst))})
	}
}

// stmtTempFix is the fix for a view of an owner temporary that outlives
// its statement: keep the owner in a variable, which lives until the end
// of its scope.
func stmtTempFix(l *loc) *diag.Fix {
	what := "..."
	if e, ok := l.node.(ast.Expr); ok {
		what = exprText(e)
	}
	if l.object || l.method == "" {
		return pu.New(diag.Fix{Intro: "Keep the owner in a variable, so that it lives until the end of its scope, and take the view through that variable", Code: compat.Sprintfb("v := %s\n... v ...", compat.ArgS(what))})
	}
	return pu.New(diag.Fix{
		Intro: "Keep the owner in a variable, so that it lives until the end of its scope, and call the method on the variable",
		Code:  compat.Sprintfb("v := %s\n... v.%s() ...", compat.ArgS(what), compat.ArgS(l.method)),
	})
}

func (r *reporter) passFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
	if l.kind == lkLoopBody {
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Change %s to call yield only while it runs, without keeping it (language spec section 12.11).", compat.ArgS(e.callee))})
		return
	}
	if l.isVarLoc() {
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("If %s should keep its own copy of %s, change it to take the value, not a pointer, or an owner it can keep: an owned[%s] from new, a shared[%s], or a library owner such as a vec.Vec.", compat.ArgS(e.callee), compat.ArgS(l.v.Name()), compat.ArgS(varType(l, q)), compat.ArgS(varType(l, q)))})
		return
	}
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Change %s to take a value or an owner, which it can keep: an owned[T] from new, a shared[T], or a library owner such as a vec.Vec.", compat.ArgS(e.callee))})
}

func handOffFixes(d *diag.Diagnostic, e *viewErr, l *loc, how string) {
	if !addrOfVar(e.expr, l) {
		d.Fix = pu.New(diag.Fix{Intro: "Pass a value, which is copied, or an owner, which moves, instead of a view of the frame."})
		return
	}
	val := strings.TrimPrefix(exprText(e.expr), "&")
	code := ""
	switch how {
	case "task":
		if e.call != nil {
			var args []string
			for _, arg := range e.call.Args {
				if arg == e.expr {
					args = compat.Append(args, val)
				} else {
					args = compat.Append(args, exprText(arg))
				}
			}
			code = compat.Sprintfb("task %s(%s)", compat.ArgS(exprText(e.call.Fun)), compat.ArgS(compat.Join(args, ", ")))
		}
	case "send":
		code = compat.Sprintfb("%s <- %s", compat.ArgS(exprText(e.ch)), compat.ArgS(val))
	}
	if code == "" {
		d.Fix = pu.New(diag.Fix{Intro: "Pass the value itself. This copies it, or moves it if it is an owner."})
		return
	}
	d.Fix = pu.New(diag.Fix{Intro: "Pass the value itself. This copies it, or moves it if it is an owner", Code: code})
}

// loopDiag explains a view of a loop's storage kept past the iteration. The
// view is kept either by a store in the function itself (e.dst says where)
// or by a callee (e.callee and e.calleeWhy say what it does).
func (r *reporter) loopDiag(d *diag.Diagnostic, e *viewErr, x string, add func(string)) {
	l, q := e.l, types2.RelativeTo(e.qual)
	line := 0
	if e.loop != nil {
		line = r.line(e.loop.Pos())
	}
	head := compat.Sprintfb("cannot keep %s in %s after this iteration", compat.ArgS(x), compat.ArgS(e.dst))
	holder := e.dst
	var keeps string
	if e.callee != "" {
		head = compat.Sprintfb("cannot pass %s to %s, which keeps it after this iteration", compat.ArgS(x), compat.ArgS(e.callee))
		holder = compat.Concat("what ", e.callee, " kept")
		keeps = compat.Sprintfb("%s %s.", compat.ArgS(e.callee), compat.ArgS(e.calleeWhy))
	}
	if v := e.iterVar; l.kind == lkClosure && v != nil {
		name, T := v.Name(), typeText(v.Type(), q)
		kept := compat.Concat("the literal kept in ", e.dst)
		if e.callee != "" {
			kept = compat.Concat("the literal kept by ", e.callee)
		}
		d.Msg = compat.Sprintfb("%s: it uses %s, which the loop at line %d reuses in every iteration", compat.ArgS(head), compat.ArgS(name), compat.ArgI(int64(line)))
		add(sentence(
			compat.Sprintfb("A function literal refers to the variables it uses where they live, and %s lives in %s frame, where every iteration of the loop at line %d reuses the same memory for it.", compat.ArgS(name), compat.ArgS(possessive(l.fn)), compat.ArgI(int64(line))),
			keeps,
			compat.Sprintfb("When the next iteration starts, %s would see that iteration's %s, not this one's.", compat.ArgS(kept), compat.ArgS(name))))
		saved := compat.Sprintfb("var saved %s // declared before the loop\n...\nsaved = %s", compat.ArgS(T), compat.ArgS(name))
		if e.callee != "" {
			d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("If %s needs the function literal only during the call, change it so it does not keep it. To use %s later, keep its value instead of the function literal", compat.ArgS(e.callee), compat.ArgS(name)), Code: saved})
			d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or have the function literal take %s as a parameter, and pass it where the literal is called.", compat.ArgS(name))})})
			return
		}
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("If the function literal is needed only in this iteration, call it before the iteration ends. To use %s later, keep its value instead of the function literal", compat.ArgS(name)), Code: saved})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or pass %s to the function literal as a parameter when it is called", compat.ArgS(name)), Code: compat.Sprintfb("%s = func(%s %s) { ... }\n...\n%s(%s)", compat.ArgS(e.dst), compat.ArgS(name), compat.ArgS(T), compat.ArgS(e.dst), compat.ArgS(name))})})
		return
	}
	if l.isVarLoc() {
		name := l.v.Name()
		d.Msg = compat.Sprintfb("%s: the loop at line %d reuses %s in every iteration", compat.ArgS(head), compat.ArgI(int64(line)), compat.ArgS(name))
		add(sentence(r.subject(x, e.expr, l, q),
			compat.Sprintfb("%s lives in %s frame, where every iteration of the loop at line %d reuses the same memory for it.", compat.ArgS(name), compat.ArgS(possessive(l.fn)), compat.ArgI(int64(line))),
			keeps,
			compat.Sprintfb("When the next iteration starts, %s would see that iteration's %s, not this one's.", compat.ArgS(holder), compat.ArgS(name))))
		saved := compat.Sprintfb("var saved %s // declared before the loop\n...\nsaved = %s", compat.ArgS(varType(l, q)), compat.ArgS(name))
		if e.callee != "" {
			d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("If %s needs the view only during the call, change it so it does not keep it. To use the value later, keep the value instead of a view of it", compat.ArgS(e.callee)), Code: saved})
			return
		}
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("If %s is needed only in this iteration, use it before the iteration ends. To use the value later, keep the value instead of a view of it", compat.ArgS(e.dst)), Code: saved})
		return
	}
	d.Msg = compat.Sprintfb("%s: the loop at line %d makes it in the same memory in every iteration", compat.ArgS(head), compat.ArgI(int64(line)))
	add(r.intro(l, q))
	add(sentence(compat.Sprintfb("Every iteration of the loop at line %d reuses that memory.", compat.ArgI(int64(line))), keeps,
		compat.Sprintfb("When the next iteration starts, %s would see that iteration's value, not this one's.", compat.ArgS(holder))))
	if e.callee != "" {
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Change %s so it does not keep the view, or pass a copy of the value instead of a view of it.", compat.ArgS(e.callee))})
		return
	}
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Use %s before the iteration ends, or keep a copy of the value instead of a view of it.", compat.ArgS(x))})
}

// regionLoopDiag explains a closure that a call in a loop returns, kept past
// the iteration (E3101). The text is that of the lowerer's check of the
// same rule (internal/lower, closureflow.go), which reports it when this
// analysis does not.
func (r *reporter) regionLoopDiag(d *diag.Diagnostic, e *viewErr, add func(string)) {
	call := "the call"
	var c *ast.CallExpr
	if ce, ok := e.l.node.(*ast.CallExpr); ok {
		c = ce
		call = shortExpr(ce)
	}
	where := e.dst
	if where == "" {
		where = "memory reached through a pointer"
	}
	d.Msg = compat.Sprintfb("cannot keep the closure %s returns past this iteration of the loop: it is stored in %s", compat.ArgS(call), compat.ArgS(where))
	text := compat.Sprintfb("A function that returns a closure builds it in a region its caller provides (spec section 11.5). The region of a call in a loop is part of this frame, and the call uses the same region on every iteration, so the closure %s returns on one iteration is overwritten on the next. Keeping it past the iteration, in %s, would leave it changing under you.", compat.ArgS(call), compat.ArgS(where))
	fix := "\nUse the closure within the iteration, or declare the variable that holds it inside the loop."
	if c != nil && e.iterVar != nil {
		for _, a := range c.Args {
			if mentions(a, e.iterVar, r.info(e.pos)) {
				text = compat.Concat(text, compat.Sprintfb(" Here the closure is passed back to %s, so each one would hold the one the call made before, in the region the call is about to refill: the chain grows with every iteration, and no region of fixed size can hold it.", compat.ArgS(shortExpr(c.Fun))))
				fix = "\nCompose a fixed number of closures, or keep what each step needs in a slice and return one closure that applies every step."
				break
			}
		}
	}
	add(compat.Concat(text, fix))
}

// shortExpr writes a call for a message, shortened as the lowerer does.
func shortExpr(e ast.Expr) string {
	s := types2.ExprString(e)
	if len(s) <= 40 && !strings.Contains(s, " literal)") {
		return s
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		return compat.Concat(types2.ExprString(e.Fun), "(...)")
	case *ast.CompositeLit:
		if e.Type != nil {
			return compat.Concat(types2.ExprString(e.Type), "{...}")
		}
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return compat.Concat("&", shortExpr(e.X))
		}
	}
	return s
}

// arenaFixes are the fixes for memory from a local arena that would outlive
// it.
func arenaFixes(d *diag.Diagnostic) {
	d.Fix = pu.New(diag.Fix{
		Intro: "Allocate what must outlive the arena from an allocator that does, passed explicitly",
		Code:  "heap := alloc.Default() // before alloc.Use\n...\nv := vec.New[T](heap)",
	})
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or install an arena that outlives the result, such as one the caller passes in, or make the copy that must outlive the arena after the Restore is dropped."})})
}
