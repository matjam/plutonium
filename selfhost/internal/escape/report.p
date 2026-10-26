// Translated by go2pu from internal/escape/report.go at the go-cutover tag; edited by hand since.

package escape

import (
	"pluto/path/filepath"
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/diag"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
)

// Messages. Every escape is reported as a view of the frame (a pointer, a
// slice, a closure, an interface's copy) reaching a place that outlives
// what it looks at. The message names the expression, what it looks at and
// where that lives, says why the place outlives it, and shows fixes as code
// (language spec section 21.1).

// A sinkKind says where a view of the frame went.
type sinkKind uint8

const (
	sinkReturn sinkKind = iota // returned
	sinkStore                  // stored in memory that outlives the frame
	sinkCall                   // passed to a function that keeps it
	sinkGo                     // passed to a task statement
	sinkSend                   // sent on a channel
	sinkLoop                   // kept in a variable the next iteration of a loop can see
	sinkBox                    // converted to an interface, whose copy stays in the frame until the function returns
)

// A storeKind says why the memory a view is stored in outlives it.
type storeKind uint8

const (
	storeGlobal storeKind = iota // a package variable
	storeParam                   // memory reached through a parameter
	storeHeap                    // memory that may be on the heap
	storeOuter                   // the frame of the function around a literal
	storeStmt                    // anything that outlives the statement making an owner temporary
)

// A viewErr is an escape, with what a message needs to explain it.
type viewErr struct {
	sink sinkKind
	pos  token.Pos
	expr ast.Expr // the expression holding the view, if known
	l    *loc     // what it looks at

	fn string // the function the view leaves (sinkReturn), or whose frame it is in

	// sinkStore and sinkLoop: where it is stored.
	dst   string
	where storeKind
	inMap string // a map's key, element or literal is stored into, "key" or "element", or ""

	via string // storeParam: the parameter the memory is reached through

	// sinkCall, and sinkLoop when a callee keeps the view (dst is then
	// empty): the function called and what it does with the view.
	callee    string
	calleeWhy *reason
	// sinkGo and sinkSend: the call or the channel, as written.
	call *ast.CallExpr
	ch   ast.Expr

	loop    ast.Node        // sinkLoop: the loop whose iterations reuse l
	iterVar *types2.Var     // sinkLoop with a closure: the loop's variable it uses
	qual    *types2.Package // the package the messages are relative to
}

// report records e as a diagnostic, once per key.
func (w *walker) report(e *viewErr, key string) {
	if !w.a.report {
		return
	}
	if w.countedAsReport(e) {
		return
	}
	if e.l != nil && e.l.kind == lkArena && (e.sink == sinkCall || e.sink == sinkStore && e.where == storeHeap) {
		// Memory from a local arena stored into other heap memory, by this
		// function or a callee: that memory was most likely allocated in
		// the same window, from the same arena, and the analysis cannot
		// tell. The arena's epochs are to catch the rest at run time
		// (arena.go).
		return
	}
	if e.qual == nil {
		e.qual = w.f.pkg.Types
	}
	w.a.add(e.pos, compat.Sprintf("%p|%s", w.f, key), func() *diag.Diagnostic { return w.a.viewDiag(e) })
}

// exprText writes an expression for a message.
func exprText(e ast.Expr) string {
	if e == nil {
		return ""
	}
	if _, ok := ast.Unparen(e).(*ast.FuncLit); ok {
		return "the function literal"
	}
	if cl, ok := ast.Unparen(e).(*ast.CompositeLit); ok && cl.Type == nil {
		return "{...}" // an element of a literal, its type elided
	}
	s := types2.ExprString(e)
	return strings.ReplaceAll(s, "…", "...")
}

// basePos writes a position as file:line, without the directory.
func (a *analyzer) basePos(p token.Pos) string {
	pp := a.fset.Position(p)
	return compat.Sprintfb("%s:%d", compat.ArgS(filepath.Base(pp.Filename)), compat.ArgI(int64(pp.Line)))
}

func (a *analyzer) line(p token.Pos) int { return a.fset.Position(p).Line }

func possessive(name string) string { return compat.Concat(name, "'s") }

// isArray reports whether a variable holds an array.
func isArrayVar(v *types2.Var) bool {
	if v == nil {
		return false
	}
	_, ok := underlying(v.Type()).(*types2.Array)
	return ok
}

// kindWord names what kind of variable l is: "a local variable", "a
// parameter", "a local array".
func (l *loc) kindWord() string {
	switch l.kind {
	case lkParam:
		if isArrayVar(l.v) {
			return "an array parameter"
		}
		return "a parameter"
	case lkResult, lkResultN:
		return "a result"
	}
	if isArrayVar(l.v) {
		return "a local array"
	}
	return "a local variable"
}

// isVarLoc reports whether l is a variable the program declares.
func (l *loc) isVarLoc() bool {
	return l.kind == lkVar || l.kind == lkParam || l.kind == lkResult
}

// name is what a message calls l in "which outlives x".
func (l *loc) name() string {
	switch {
	case l.isVarLoc(), (l.kind == lkObject || l.kind == lkArena) && l.v != nil:
		return l.v.Name()
	case l.kind == lkClosure && len(l.captures) > 0:
		return l.captures[0].Name()
	case l.kind == lkStmtTemp:
		return "the temporary"
	}
	return compat.Concat(possessive(l.fn), " frame")
}

// captureList names the variables a closure uses: "total", "a and b".
func (l *loc) captureList() string {
	names := compat.Make[string](len(l.captures))
	for i, v := range l.captures {
		names[i] = v.Name()
	}
	switch len(names) {
	case 0:
		return "variables"
	case 1:
		return names[0]
	}
	return compat.Concat(compat.Join(names[:len(names)-1], ", "), " and ", names[len(names)-1])
}

func (l *loc) captureNoun() string {
	if len(l.captures) == 1 {
		return compat.Concat("a local variable of ", l.fn)
	}
	return compat.Concat("local variables of ", l.fn)
}

// noun describes l with its name: "x, a local variable of load".
func (a *analyzer) noun(l *loc, q types2.Qualifier) string {
	frame := compat.Concat(possessive(l.fn), " frame")
	switch l.kind {
	case lkVar, lkParam, lkResult:
		return compat.Concat(l.v.Name(), ", ", l.kindWord(), " of ", l.fn)
	case lkResultN:
		return compat.Concat("a result of ", l.fn)
	case lkTemp:
		return compat.Concat("a temporary copy in ", frame)
	case lkRecv:
		return compat.Concat("a temporary copy of a receiver in ", frame)
	case lkStmtTemp:
		return compat.Concat("the temporary ", typeText(l.typ, q), " that ", exprText(l.node.(ast.Expr)), " returns")
	case lkLit:
		return compat.Concat("a composite literal in ", frame)
	case lkSliceLit:
		return compat.Concat("the elements of a slice literal, in ", frame)
	case lkVarargs:
		return compat.Concat("the variadic arguments of a call, in ", frame)
	case lkBox:
		return compat.Concat("a copy of ", article(typeText(l.typ, q)), " in ", frame)
	case lkClosure:
		return compat.Concat("a function literal that uses ", l.captureList(), ", ", l.captureNoun())
	case lkMethodValue:
		return compat.Concat("a method value whose receiver is in ", frame)
	case lkCtor:
		return compat.Concat("the closure ", exprText(l.node.(ast.Expr)), " returns, built in ", frame)
	case lkLoopBody:
		return compat.Concat("the loop body, a closure built in ", frame)
	case lkObject:
		if l.v != nil {
			return compat.Concat("the object ", l.v.Name(), " refers to, which ", l.v.Name(), " keeps alive in ", l.fn)
		}
		return compat.Concat("the object of a temporary ", typeText(l.typ, q), " in ", frame)
	case lkArena:
		if l.v != nil {
			return compat.Concat("memory allocated from ", l.v.Name(), ", a local arena of ", l.fn)
		}
		return compat.Concat("memory allocated from a local arena in ", frame)
	}
	return frame
}

func typeText(T types2.Type, q types2.Qualifier) string {
	if T == nil {
		return "value"
	}
	return types2.TypeString(T, q)
}

// article puts "a" or "an" before a noun.
func article(s string) string {
	if s != "" && strings.ContainsRune("aeiouAEIOU", rune(s[0])) {
		return compat.Concat("an ", s)
	}
	return compat.Concat("a ", s)
}

// intro explains where l lives, in a sentence or two.
func (a *analyzer) intro(l *loc, q types2.Qualifier) string {
	at := a.basePos(l.pos)
	switch l.kind {
	case lkVar, lkParam, lkResult:
		s := compat.Sprintfb("%s is %s of %s (declared at %s).", compat.ArgS(l.v.Name()), compat.ArgS(l.kindWord()), compat.ArgS(l.fn), compat.ArgS(at))
		if l.kind != lkVar {
			s = compat.Concat(s, compat.Concat(" It lives in ", possessive(l.fn), " frame, like a local variable."))
		}
		return s
	case lkResultN:
		return compat.Sprintfb("The result lives in %s frame.", compat.ArgS(possessive(l.fn)))
	case lkTemp:
		return compat.Sprintfb("Taking the address of a value that is not a variable (at %s) copies the value to a temporary in %s frame.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkRecv:
		return compat.Sprintfb("Calling a pointer method on a value that is not a variable (at %s) copies the value to a temporary in %s frame, and the method gets a pointer to the copy.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkStmtTemp:
		if l.object {
			return compat.Sprintfb("The %s made at %s is a temporary: nothing keeps it, so its object is dropped at the end of the statement. A view of the object may be passed to calls in that statement but may not outlive it.", compat.ArgS(typeText(l.typ, q)), compat.ArgS(at))
		}
		return compat.Sprintfb("The %s that %s returns (at %s) is a temporary: nothing keeps it, so it is the receiver of the method only until the end of the statement. A view of it may be passed to calls in that statement but may not outlive it.", compat.ArgS(typeText(l.typ, q)), compat.ArgS(exprText(l.node.(ast.Expr))), compat.ArgS(at))
	case lkLit:
		return compat.Sprintfb("A composite literal whose address is taken (at %s) is made in %s frame, as a local variable would be.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkSliceLit:
		return compat.Sprintfb("A slice literal (at %s) keeps its elements in an array in %s frame, and the slice looks at that array.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkVarargs:
		return compat.Sprintfb("A call with variadic arguments (at %s) passes them in an array in %s frame, and the function gets a slice of that array.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkBox:
		return compat.Sprintfb("Converting a value larger than one word to an interface puts a copy of the value in the frame of the function that converts it (here %s, at %s), and the interface points at that copy.", compat.ArgS(l.fn), compat.ArgS(at))
	case lkClosure:
		decl := ""
		if len(l.captures) > 0 {
			decl = compat.Sprintfb(" (%s is declared at %s)", compat.ArgS(l.captures[0].Name()), compat.ArgS(a.basePos(l.captures[0].Pos())))
		}
		return compat.Sprintfb("A function literal that uses variables of the function around it refers to those variables where they live, in %s frame%s.", compat.ArgS(possessive(l.fn)), compat.ArgS(decl))
	case lkMethodValue:
		return compat.Sprintfb("A method value (at %s) is a record in %s frame that holds its receiver, and calling it calls the method on that receiver.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkLoopBody:
		return compat.Sprintfb("A range loop over a function (at %s) calls the function with a yield function whose body is the loop body, a closure built in %s frame (section 12.11); yield may be called only while the loop runs.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkCtor:
		return compat.Sprintfb("%s returns a closure, which it builds in a region of its caller's frame, %s (at %s); the closure lives as long as that frame (section 4.5).", compat.ArgS(exprText(l.node.(ast.Expr))), compat.ArgS(possessive(l.fn)), compat.ArgS(at))
	case lkArena:
		if l.explicit {
			return compat.Sprintfb("%s (a local arena of %s, declared at %s) was given as an allocator to a call that returns a string, or to a value that returns one, and such text may have been allocated from the arena. What lives in the arena is gone when the arena is.", compat.ArgS(l.v.Name()), compat.ArgS(l.fn), compat.ArgS(at))
		}
		name := "a local arena"
		if l.v != nil {
			name = compat.Concat(l.v.Name(), " (a local arena of ", l.fn, ")")
		}
		return compat.Sprintfb("%s (at %s) makes %s the default allocator, so what is allocated through the default allocator from there on (by new, a string operation, or a call that allocates and leaves out its allocator) lives in the arena, and is gone when the arena is.", compat.ArgS(exprText(l.node.(ast.Expr))), compat.ArgS(at), compat.ArgS(name))
	case lkObject:
		ref := refNoun(l.typ)
		if l.v != nil && ref != "" {
			what := "a table"
			if ref == "channel" {
				what = "a channel"
			}
			return compat.Sprintfb("%s (declared at %s) is %s, a counted reference to %s: an ordinary interface that lends the %s does not count a reference of its own, so it may not outlive %s, whose reference is dropped at the end of its scope in %s.", compat.ArgS(l.v.Name()), compat.ArgS(at), compat.ArgS(article(typeText(l.typ, q))), compat.ArgS(what), compat.ArgS(ref), compat.ArgS(l.v.Name()), compat.ArgS(l.fn))
		}
		if l.v != nil {
			return compat.Sprintfb("%s (declared at %s) is %s: the object it refers to lives only while %s holds it, and is dropped when %s is dropped at the end of its scope in %s, or released when it is the last reference. A pointer into the object, or an interface that lends it, is a view that may not outlive %s.", compat.ArgS(l.v.Name()), compat.ArgS(at), compat.ArgS(article(typeText(l.typ, q))), compat.ArgS(l.v.Name()), compat.ArgS(l.v.Name()), compat.ArgS(l.fn), compat.ArgS(l.v.Name()))
		}
		if ref != "" {
			return compat.Sprintfb("The %s made at %s is a temporary: nothing keeps a reference to it after the statement, and an ordinary interface that lends it counts no reference of its own, so the interface may not outlive that statement.", compat.ArgS(typeText(l.typ, q)), compat.ArgS(at))
		}
		return compat.Sprintfb("The %s made at %s is a temporary: nothing keeps it, so its object is dropped at the end of the statement, and a view of the object may not outlive that statement.", compat.ArgS(typeText(l.typ, q)), compat.ArgS(at))
	}
	return ""
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
func (a *analyzer) viewPhrase(x string, e ast.Expr, l *loc, q types2.Qualifier) string {
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
	return compat.Concat(", which points into ", a.noun(l, q))
}

// subject is x as the subject of a sentence about where it points.
func (a *analyzer) subject(x string, e ast.Expr, l *loc, q types2.Qualifier) string {
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
	case l.kind == lkBox:
		return compat.Concat(x, " is an interface that points at a copy of its value in ", possessive(l.fn), " frame.")
	case l.kind == lkObject && l.v != nil && refNoun(l.typ) != "" && x == l.v.Name():
		return ""
	}
	return compat.Concat(x, " points into ", a.noun(l, q), ".")
}

// viewDiag builds the diagnostic for e.
func (a *analyzer) viewDiag(e *viewErr) *diag.Diagnostic {
	l, q := e.l, types2.RelativeTo(e.qual)
	x := exprText(e.expr)
	if x == "" {
		x = defaultText(l)
	}
	d := pu.New(diag.Diagnostic{Pos: a.fset.Position(e.pos)})
	var text []string
	add := func(s string) {
		if s != "" {
			text = compat.Append(text, s)
		}
	}
	// "When f returns, x's memory is reclaimed".
	reclaimed := compat.Sprintfb("When %s returns, %s memory is reclaimed", compat.ArgS(l.fn), compat.ArgS(possessive(l.name())))
	switch {
	case l.kind == lkArena:
		reclaimed = compat.Sprintfb("When %s returns, the arena %s is gone", compat.ArgS(l.fn), compat.ArgS(l.name()))
	case l.kind == lkObject && l.v != nil && refNoun(l.typ) != "":
		reclaimed = compat.Sprintfb("When %s returns, %s drops its reference, and the %s is freed if that was the last one", compat.ArgS(l.fn), compat.ArgS(l.v.Name()), compat.ArgS(refNoun(l.typ)))
	case l.kind == lkObject && l.v != nil:
		reclaimed = compat.Sprintfb("When %s returns, %s is dropped and the object freed", compat.ArgS(l.fn), compat.ArgS(l.v.Name()))
	case l.kind == lkObject:
		reclaimed = "At the end of the statement, the temporary is dropped and its object freed"
	case l.kind == lkStmtTemp:
		reclaimed = "At the end of the statement, the temporary is dropped"
	case !l.isVarLoc() && l.kind != lkClosure:
		reclaimed = compat.Sprintfb("When %s returns, its frame is reclaimed", compat.ArgS(l.fn))
	}
	// What would be left pointing at reclaimed memory.
	pointer := x
	if l.kind == lkBox {
		pointer = "the interface"
	}
	about := sentence(a.subject(x, e.expr, l, q), a.intro(l, q))

	switch e.sink {
	case sinkReturn:
		d.Code = diag.ReturnFrameView
		d.Msg = compat.Concat("cannot return ", x, a.viewPhrase(x, e.expr, l, q))
		if l.kind == lkBox && e.expr != nil && !isInterfaceExpr(a, e.expr) {
			d.Msg = compat.Sprintfb("cannot return %s as an interface: the interface would hold a copy of it in %s frame", compat.ArgS(x), compat.ArgS(possessive(l.fn)))
		}
		var ref string // "map" or "channel" when the interface lends one
		var exprT types2.Type
		if e.expr != nil {
			exprT = a.types[e.expr].Type
		}
		lends := l.kind == lkObject && e.expr != nil && !isInterfaceExpr(a, e.expr) && (isBoxPtr(exprT) || refNoun(exprT) != "")
		if lends {
			ref = refNoun(exprT)
		}
		lendsRef := ref != ""
		if lends {
			d.Msg = compat.Sprintfb("cannot return %s as an interface: an ordinary interface only borrows the box, which is dropped when %s returns", compat.ArgS(x), compat.ArgS(e.fn))
			if lendsRef {
				d.Msg = compat.Sprintfb("cannot return %s as an interface: an ordinary interface only borrows the %s, whose reference %s drops when it returns", compat.ArgS(x), compat.ArgS(ref), compat.ArgS(e.fn))
			}
		}
		switch {
		case l.kind == lkArena:
			add(sentence(about, compat.Sprintfb("%s, so %s would point at memory that is no longer allocated.", compat.ArgS(reclaimed), compat.ArgS(x))))
		case ref == "map":
			add(sentence(about, compat.Sprintfb("Converting %s to an ordinary interface lends it: the interface points at the map's table without a reference of its own.", compat.ArgS(x))))
		case ref == "channel":
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
		if lendsRef {
			d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the %s itself, changing the result type to %s: the caller gets a reference of its own", compat.ArgS(ref), compat.ArgS(typeText(exprT, q))), Code: compat.Concat("return ", x)})
		} else if lends {
			name := x
			d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the box itself, changing the result type to %s", compat.ArgS(typeText(a.types[e.expr].Type, q))), Code: compat.Concat("return ", name)})
			d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or declare the interface as an owning interface, which takes ownership of what is converted into it (language spec section 18.5)", Code: "type I owner interface {\n\t...\n}"})})
		} else {
			a.returnFixes(d, e, l, q)
		}
	case sinkStore:
		d.Code = diag.StoreFrameView
		d.Msg = compat.Sprintfb("cannot store %s in %s, which outlives %s", compat.ArgS(x), compat.ArgS(e.dst), compat.ArgS(l.name()))
		var why string
		switch e.where {
		case storeGlobal:
			why = compat.Sprintfb("%s, but package variables live for the whole program, so %s would be left pointing at reclaimed memory.", compat.ArgS(reclaimed), compat.ArgS(e.dst))
		case storeParam:
			why = compat.Sprintfb("%s, but %s is reached through %s, which points to memory that %s caller owns and that lives longer. It would be left pointing at reclaimed memory.", compat.ArgS(reclaimed), compat.ArgS(e.dst), compat.ArgS(e.via), compat.ArgS(possessive(e.fn)))
		case storeHeap:
			why = compat.Sprintfb("%s, but %s may be on the heap and live longer, so it would be left pointing at reclaimed memory.", compat.ArgS(reclaimed), compat.ArgS(e.dst))
			if e.inMap != "" {
				why = compat.Sprintfb("%s, but a map keeps its keys and elements in a table on the heap, which every copy of the map shares and which may live longer, so it would be left pointing at reclaimed memory.", compat.ArgS(reclaimed))
			}
		case storeOuter:
			why = compat.Sprintfb("The function literal's frame is reclaimed each time the literal returns, but %s belongs to the function around it and lives longer.", compat.ArgS(e.dst))
		case storeStmt:
			why = compat.Sprintfb("%s, but %s lives longer, so it would be left pointing at a value that has been dropped.", compat.ArgS(reclaimed), compat.ArgS(e.dst))
		}
		add(sentence(about, why))
		a.storeFixes(d, e, l, q)
	case sinkCall:
		d.Code = diag.PassFrameView
		d.Msg = compat.Sprintfb("cannot pass %s to %s, which keeps it after returning", compat.ArgS(x), compat.ArgS(e.callee))
		add(sentence(about, compat.Sprintfb("%s %s, so %s would outlive %s.", compat.ArgS(e.callee), compat.ArgS(e.calleeWhy.String()), compat.ArgS(pointer), compat.ArgS(l.name()))))
		a.passFixes(d, e, l, q)
	case sinkGo:
		d.Code = diag.GoFrameView
		d.Msg = compat.Sprintfb("cannot pass %s to a task statement: the new task may outlive %s", compat.ArgS(x), compat.ArgS(l.name()))
		add(sentence(about, compat.Sprintfb("The task started here runs on its own, and may still be running after %s returns and %s memory is reclaimed.", compat.ArgS(e.fn), compat.ArgS(possessive(l.name())))))
		a.handOffFixes(d, e, l, "task")
		if e.call != nil {
			d.More = compat.Append(d.More, pu.New(diag.Fix{Intro: compat.Sprintfb("Or start it with a sync.Scope, which waits for its tasks before its block ends, so that they may use %s variables", compat.ArgS(possessive(e.fn))),
				Code: compat.Sprintfb("var s sync.Scope\ns.Task(func() { %s })", compat.ArgS(exprText(e.call)))}))
		}
	case sinkSend:
		d.Code = diag.SendFrameView
		d.Msg = compat.Sprintfb("cannot send %s on %s: the receiver may outlive %s", compat.ArgS(x), compat.ArgS(exprText(e.ch)), compat.ArgS(l.name()))
		add(sentence(about, compat.Sprintfb("The value sent here may be received after %s returns and %s memory is reclaimed.", compat.ArgS(e.fn), compat.ArgS(possessive(l.name())))))
		a.handOffFixes(d, e, l, "send")
	case sinkLoop:
		d.Code = diag.LoopFrameView
		a.loopDiag(d, e, x, add)
	case sinkBox:
		d.Code = diag.StaleView
		d.Msg = compat.Sprintfb("cannot convert %s to an interface: it points into %s, which is gone before the interface's copy is released", compat.ArgS(x), compat.ArgS(a.noun(l, q)))
		add(sentence(about, compat.Sprintfb("Converting %s to an interface puts a copy of it in %s frame, and the copy holds a reference to the text until %s returns. That is after the arena %s is dropped, and releasing the reference would write into the arena's memory.", compat.ArgS(x), compat.ArgS(possessive(e.fn)), compat.ArgS(e.fn), compat.ArgS(l.name()))))
		d.Fix = pu.New(diag.Fix{
			Intro: "Use the text without converting it to an interface, for example by writing its bytes",
			Code:  "os.Stdout.Write(b.Bytes())",
		})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or convert a string made from an allocator that outlives the arena, passed explicitly, or from the default allocator before the arena is installed"})})
	}
	d.Text = compat.Join(text, "\n")
	if l.kind == lkStmtTemp && e.sink != sinkLoop {
		d.Code = diag.TempViewKept
	}
	return d
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

func isInterfaceExpr(a *analyzer, e ast.Expr) bool {
	tv, ok := a.types[e]
	return ok && tv.Type != nil && isInterface(tv.Type)
}

// defaultText is how a message writes a view of l when it has no
// expression for it.
func defaultText(l *loc) string {
	switch l.kind {
	case lkVar, lkParam, lkResult:
		return compat.Concat("&", l.v.Name())
	case lkClosure:
		return "the function literal"
	case lkResultN:
		return "the result"
	case lkVarargs:
		if call, ok := l.node.(*ast.CallExpr); ok {
			return compat.Concat("the variadic arguments of ", exprText(call.Fun))
		}
	}
	if l.node != nil {
		if e, ok := l.node.(ast.Expr); ok {
			return exprText(e)
		}
	}
	return compat.Concat("a pointer into ", possessive(l.fn), " frame")
}

// elemOf returns the type a variable's view looks at: x's type for &x.
func varType(l *loc, q types2.Qualifier) string {
	if l.v != nil {
		return typeText(l.v.Type(), q)
	}
	return typeText(l.typ, q)
}

func (a *analyzer) returnFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
	switch {
	case l.kind == lkClosure:
		var fields []string
		for _, v := range l.captures {
			fields = compat.Append(fields, compat.Concat(v.Name(), " ", typeText(v.Type(), q)))
		}
		d.Fix = pu.New(diag.Fix{
			Intro: "Return a value that carries its own state instead, such as a struct with a method",
			Code:  compat.Sprintfb("type state struct{ %s }\n\nfunc (s *state) Call() { ... } // uses s.%s", compat.ArgS(compat.Join(fields, "; ")), compat.ArgS(l.captures[0].Name())),
		})
	case l.kind == lkMethodValue:
		d.Fix = pu.New(diag.Fix{Intro: "Return the receiver itself, and let the caller call the method on it."})
	case l.kind == lkBox:
		d.Fix = pu.New(diag.Fix{
			Intro: compat.Sprintfb("Return a pointer to memory that outlives %s, such as a field of a longer-lived value", compat.ArgS(e.fn)),
			Code:  compat.Sprintfb("p.err = %s{...}\nreturn &p.err", compat.ArgS(typeText(l.typ, q))),
		})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "or return a value that fits in one word, such as an error code."})})
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
	case l.kind == lkObject && l.v != nil:
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return %s itself: an owned box moves to the caller, and a shared or synced one gives the caller its own reference", compat.ArgS(name)), Code: compat.Concat("return ", name)})
	case l.kind == lkObject:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the box itself, changing the result type to %s", compat.ArgS(typeText(l.typ, q))), Code: compat.Concat("return ", exprText(l.node.(ast.Expr)))})
	case l.kind == lkArena:
		arenaFixes(d, l)
	case l.kind == lkStmtTemp:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return the %s itself, which keeps its memory alive in the caller, or a copy of what the view looks at.", compat.ArgS(typeText(l.typ, q)))})
	case l.isVarLoc():
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Return values instead of views of %s, or allocate %s and return its owner, an owned[%s]", compat.ArgS(name), compat.ArgS(name), compat.ArgS(varType(l, q))), Code: compat.Sprintfb("%s := new(%s)\n...\nreturn %s", compat.ArgS(name), compat.ArgS(varType(l, q)), compat.ArgS(name))})
	default:
		d.Fix = pu.New(diag.Fix{Intro: "Return the value itself, which copies it, or allocate it so that it outlives the function."})
	}
}

func (a *analyzer) storeFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
	switch {
	case e.inMap != "":
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store the value itself instead of a view of %s, changing the map's %s type to match.", compat.ArgS(l.name()), compat.ArgS(e.inMap))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or keep what the map refers to in memory that lives as long as the map, such as a package variable, or in a counted reference (shared[T] or synced[T]) stored in the map."})})
	case l.kind == lkClosure:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store a value that carries its own state instead of the function literal, such as a struct with a method, or make %s a package variable if it must outlive %s.", compat.ArgS(l.captureList()), compat.ArgS(l.fn))})
	case l.isVarLoc() && addrOfVar(e.expr, l) && !strings.Contains(trimAddr(e.dst), " "):
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store the value instead of a pointer to it, changing the type of %s to match. This copies %s, or moves it if it is an owner", compat.ArgS(trimAddr(e.dst)), compat.ArgS(name)), Code: compat.Sprintfb("%s = %s", compat.ArgS(trimAddr(e.dst)), compat.ArgS(name))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or allocate %s and store its owner, an owned[%s], which moves into %s", compat.ArgS(name), compat.ArgS(varType(l, q)), compat.ArgS(trimAddr(e.dst))), Code: compat.Sprintfb("%s = new(%s)", compat.ArgS(trimAddr(e.dst)), compat.ArgS(varType(l, q)))})})
	case l.kind == lkArena:
		arenaFixes(d, l)
	case l.kind == lkStmtTemp:
		d.Fix = stmtTempFix(l)
	case l.kind == lkObject && l.v != nil && refNoun(l.typ) != "":
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store %s itself, changing the type of %s to %s: it then counts a reference of its own", compat.ArgS(name), compat.ArgS(trimAddr(e.dst)), compat.ArgS(typeText(l.typ, q))), Code: compat.Sprintfb("%s = %s", compat.ArgS(trimAddr(e.dst)), compat.ArgS(name))})
	case l.kind == lkObject && l.v != nil:
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store %s itself, changing the type of %s to match: an owned box moves there, and a shared or synced one is counted", compat.ArgS(name), compat.ArgS(trimAddr(e.dst))), Code: compat.Sprintfb("%s = %s", compat.ArgS(trimAddr(e.dst)), compat.ArgS(name))})
	case l.isVarLoc():
		name := l.v.Name()
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store values instead of views of %s, or allocate %s and store its owner, an owned[%s], which moves there", compat.ArgS(name), compat.ArgS(name), compat.ArgS(varType(l, q))), Code: compat.Sprintfb("%s = new(%s)", compat.ArgS(trimAddr(e.dst)), compat.ArgS(varType(l, q)))})
	default:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Store the value itself instead of a view of %s frame, or allocate it so that it lives as long as %s.", compat.ArgS(possessive(l.fn)), compat.ArgS(e.dst))})
	}
}

// stmtTempFix is the fix for a view of an owner temporary that outlives
// its statement: keep the owner in a variable, which lives until the end
// of its scope.
func stmtTempFix(l *loc) *diag.Fix {
	if l.object {
		return pu.New(diag.Fix{Intro: "Keep the owner in a variable, so that it lives until the end of its scope, and take the view through that variable", Code: compat.Sprintfb("v := %s\n... v ...", compat.ArgS(exprText(l.node.(ast.Expr))))})
	}
	return pu.New(diag.Fix{
		Intro: "Keep the owner in a variable, so that it lives until the end of its scope, and call the method on the variable",
		Code:  compat.Sprintfb("v := %s\n... v.%s() ...", compat.ArgS(exprText(l.node.(ast.Expr))), compat.ArgS(l.method)),
	})
}

// trimAddr turns a message's destination back into an expression:
// "package variable G" into "G".
func trimAddr(dst string) string {
	return strings.TrimPrefix(dst, "package variable ")
}

func (a *analyzer) passFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
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

func (a *analyzer) handOffFixes(d *diag.Diagnostic, e *viewErr, l *loc, how string) {
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
func (a *analyzer) loopDiag(d *diag.Diagnostic, e *viewErr, x string, add func(string)) {
	l, q := e.l, types2.RelativeTo(e.qual)
	line := a.line(e.loop.Pos())
	// head starts the message and holder is what would see the next
	// iteration's value. keeps is the callee's own sentence, if a callee
	// keeps the view.
	head := compat.Sprintfb("cannot keep %s in %s after this iteration", compat.ArgS(x), compat.ArgS(e.dst))
	holder := e.dst
	var keeps string
	if e.callee != "" {
		head = compat.Sprintfb("cannot pass %s to %s, which keeps it after this iteration", compat.ArgS(x), compat.ArgS(e.callee))
		holder = compat.Concat("what ", e.callee, " kept")
		keeps = compat.Sprintfb("%s %s.", compat.ArgS(e.callee), compat.ArgS(e.calleeWhy.String()))
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
		add(sentence(a.subject(x, e.expr, l, q),
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
	add(a.intro(l, q))
	add(sentence(compat.Sprintfb("Every iteration of the loop at line %d reuses that memory.", compat.ArgI(int64(line))), keeps,
		compat.Sprintfb("When the next iteration starts, %s would see that iteration's value, not this one's.", compat.ArgS(holder))))
	if e.callee != "" {
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Change %s so it does not keep the view, or pass a copy of the value instead of a view of it.", compat.ArgS(e.callee))})
		return
	}
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Use %s before the iteration ends, or keep a copy of the value instead of a view of it.", compat.ArgS(x))})
}

// arenaFixes are the fixes for memory from a local arena that would outlive
// it.
func arenaFixes(d *diag.Diagnostic, l *loc) {
	d.Fix = pu.New(diag.Fix{
		Intro: "Allocate what must outlive the arena from an allocator that does, passed explicitly",
		Code:  "heap := alloc.Default() // before alloc.Use\n...\nv := vec.New[T](heap)",
	})
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or install an arena that outlives the result, such as one the caller passes in, or make the copy that must outlive the arena after the Restore is dropped."})})
}
