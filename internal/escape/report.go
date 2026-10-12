package escape

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
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
	callee, calleeWhy string
	// sinkGo and sinkSend: the call or the channel, as written.
	call *ast.CallExpr
	ch   ast.Expr

	loop    ast.Node    // sinkLoop: the loop whose iterations reuse l
	iterVar *types2.Var // sinkLoop with a closure: the loop's variable it uses
	qual    types2.Qualifier
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
		e.qual = types2.RelativeTo(w.f.pkg.Types)
	}
	w.a.add(e.pos, fmt.Sprintf("%p|%s", w.f, key), func() *diag.Diagnostic { return w.a.viewDiag(e) })
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
	return fmt.Sprintf("%s:%d", filepath.Base(pp.Filename), pp.Line)
}

func (a *analyzer) line(p token.Pos) int { return a.fset.Position(p).Line }

func possessive(name string) string { return name + "'s" }

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
	return possessive(l.fn) + " frame"
}

// captureList names the variables a closure uses: "total", "a and b".
func (l *loc) captureList() string {
	names := make([]string, len(l.captures))
	for i, v := range l.captures {
		names[i] = v.Name()
	}
	switch len(names) {
	case 0:
		return "variables"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func (l *loc) captureNoun() string {
	if len(l.captures) == 1 {
		return "a local variable of " + l.fn
	}
	return "local variables of " + l.fn
}

// noun describes l with its name: "x, a local variable of load".
func (a *analyzer) noun(l *loc, q types2.Qualifier) string {
	frame := possessive(l.fn) + " frame"
	switch l.kind {
	case lkVar, lkParam, lkResult:
		return l.v.Name() + ", " + l.kindWord() + " of " + l.fn
	case lkResultN:
		return "a result of " + l.fn
	case lkTemp:
		return "a temporary copy in " + frame
	case lkRecv:
		return "a temporary copy of a receiver in " + frame
	case lkStmtTemp:
		return "the temporary " + typeText(l.typ, q) + " that " + exprText(l.node.(ast.Expr)) + " returns"
	case lkLit:
		return "a composite literal in " + frame
	case lkSliceLit:
		return "the elements of a slice literal, in " + frame
	case lkVarargs:
		return "the variadic arguments of a call, in " + frame
	case lkBox:
		return "a copy of " + article(typeText(l.typ, q)) + " in " + frame
	case lkClosure:
		return "a function literal that uses " + l.captureList() + ", " + l.captureNoun()
	case lkMethodValue:
		return "a method value whose receiver is in " + frame
	case lkCtor:
		return "the closure " + exprText(l.node.(ast.Expr)) + " returns, built in " + frame
	case lkObject:
		if l.v != nil {
			return "the object " + l.v.Name() + " refers to, which " + l.v.Name() + " keeps alive in " + l.fn
		}
		return "the object of a temporary " + typeText(l.typ, q) + " in " + frame
	case lkArena:
		if l.v != nil {
			return "memory allocated from " + l.v.Name() + ", a local arena of " + l.fn
		}
		return "memory allocated from a local arena in " + frame
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
		return "an " + s
	}
	return "a " + s
}

// intro explains where l lives, in a sentence or two.
func (a *analyzer) intro(l *loc, q types2.Qualifier) string {
	at := a.basePos(l.pos)
	switch l.kind {
	case lkVar, lkParam, lkResult:
		s := fmt.Sprintf("%s is %s of %s (declared at %s).", l.v.Name(), l.kindWord(), l.fn, at)
		if l.kind != lkVar {
			s += " It lives in " + possessive(l.fn) + " frame, like a local variable."
		}
		return s
	case lkResultN:
		return fmt.Sprintf("The result lives in %s frame.", possessive(l.fn))
	case lkTemp:
		return fmt.Sprintf("Taking the address of a value that is not a variable (at %s) copies the value to a temporary in %s frame.", at, possessive(l.fn))
	case lkRecv:
		return fmt.Sprintf("Calling a pointer method on a value that is not a variable (at %s) copies the value to a temporary in %s frame, and the method gets a pointer to the copy.", at, possessive(l.fn))
	case lkStmtTemp:
		return fmt.Sprintf("The %s that %s returns (at %s) is a temporary: nothing keeps it, so it is the receiver of the method only until the end of the statement. A view of it may be passed to calls in that statement but may not outlive it.", typeText(l.typ, q), exprText(l.node.(ast.Expr)), at)
	case lkLit:
		return fmt.Sprintf("A composite literal whose address is taken (at %s) is made in %s frame, as a local variable would be.", at, possessive(l.fn))
	case lkSliceLit:
		return fmt.Sprintf("A slice literal (at %s) keeps its elements in an array in %s frame, and the slice looks at that array.", at, possessive(l.fn))
	case lkVarargs:
		return fmt.Sprintf("A call with variadic arguments (at %s) passes them in an array in %s frame, and the function gets a slice of that array.", at, possessive(l.fn))
	case lkBox:
		return fmt.Sprintf("Converting a value larger than one word to an interface puts a copy of the value in the frame of the function that converts it (here %s, at %s), and the interface points at that copy.", l.fn, at)
	case lkClosure:
		decl := ""
		if len(l.captures) > 0 {
			decl = fmt.Sprintf(" (%s is declared at %s)", l.captures[0].Name(), a.basePos(l.captures[0].Pos()))
		}
		return fmt.Sprintf("A function literal that uses variables of the function around it refers to those variables where they live, in %s frame%s.", possessive(l.fn), decl)
	case lkMethodValue:
		return fmt.Sprintf("A method value (at %s) is a record in %s frame that holds its receiver, and calling it calls the method on that receiver.", at, possessive(l.fn))
	case lkCtor:
		return fmt.Sprintf("%s returns a closure, which it builds in a region of its caller's frame, %s (at %s); the closure lives as long as that frame (section 4.5).", exprText(l.node.(ast.Expr)), possessive(l.fn), at)
	case lkArena:
		name := "a local arena"
		if l.v != nil {
			name = l.v.Name() + " (a local arena of " + l.fn + ")"
		}
		return fmt.Sprintf("%s (at %s) makes %s the default allocator, so what is allocated through the default allocator from there on (by new, a string operation, or a call that allocates and leaves out its allocator) lives in the arena, and is gone when the arena is.", exprText(l.node.(ast.Expr)), at, name)
	case lkObject:
		ref := refNoun(l.typ)
		if l.v != nil && ref != "" {
			what := "a table"
			if ref == "channel" {
				what = "a channel"
			}
			return fmt.Sprintf("%s (declared at %s) is %s, a counted reference to %s: an ordinary interface that lends the %s does not count a reference of its own, so it may not outlive %s, whose reference is dropped at the end of its scope in %s.", l.v.Name(), at, article(typeText(l.typ, q)), what, ref, l.v.Name(), l.fn)
		}
		if l.v != nil {
			return fmt.Sprintf("%s (declared at %s) is %s: the object it refers to lives only while %s holds it, and is dropped when %s is dropped at the end of its scope in %s, or released when it is the last reference. A pointer into the object, or an interface that lends it, is a view that may not outlive %s.", l.v.Name(), at, article(typeText(l.typ, q)), l.v.Name(), l.v.Name(), l.fn, l.v.Name())
		}
		if ref != "" {
			return fmt.Sprintf("The %s made at %s is a temporary: nothing keeps a reference to it after the statement, and an ordinary interface that lends it counts no reference of its own, so the interface may not outlive that statement.", typeText(l.typ, q), at)
		}
		return fmt.Sprintf("The %s made at %s is a temporary: nothing keeps it, so its object is dropped at the end of the statement, and a view of the object may not outlive that statement.", typeText(l.typ, q), at)
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
		return ", a pointer to " + l.kindWord()
	case sliceOfVar(e, l):
		return ", a slice of " + l.v.Name() + ", " + l.kindWord()
	case l.kind == lkClosure && is(e, l):
		return ": it uses " + l.captureList() + ", " + l.captureNoun()
	case l.kind == lkLit && is(e, l):
		return ", a pointer to a composite literal in " + possessive(l.fn) + " frame"
	case l.kind == lkSliceLit && is(e, l):
		return ", a slice literal whose elements are in " + possessive(l.fn) + " frame"
	case l.kind == lkMethodValue && is(e, l):
		return ", a method value whose receiver is in " + possessive(l.fn) + " frame"
	case l.kind == lkBox:
		return ", an interface holding a copy of " + article(typeText(l.typ, q)) + " in " + possessive(l.fn) + " frame"
	case l.kind == lkClosure:
		return ", a function literal that uses " + l.captureList() + ", " + l.captureNoun()
	}
	return ", which points into " + a.noun(l, q)
}

// subject is x as the subject of a sentence about where it points.
func (a *analyzer) subject(x string, e ast.Expr, l *loc, q types2.Qualifier) string {
	switch {
	case addrOfVar(e, l):
		return x + " is a pointer to " + l.v.Name() + "."
	case sliceOfVar(e, l):
		return x + " is a slice that looks at " + l.v.Name() + "."
	case is(e, l):
		return ""
	case l.isVarLoc():
		return x + " points into " + l.v.Name() + "."
	case l.kind == lkClosure:
		return x + " is a function literal that uses " + l.captureList() + "."
	case l.kind == lkBox:
		return x + " is an interface that points at a copy of its value in " + possessive(l.fn) + " frame."
	case l.kind == lkObject && l.v != nil && refNoun(l.typ) != "" && x == l.v.Name():
		return ""
	}
	return x + " points into " + a.noun(l, q) + "."
}

// viewDiag builds the diagnostic for e.
func (a *analyzer) viewDiag(e *viewErr) *diag.Diagnostic {
	l, q := e.l, e.qual
	x := exprText(e.expr)
	if x == "" {
		x = defaultText(l)
	}
	d := &diag.Diagnostic{Pos: a.fset.Position(e.pos)}
	var text []string
	add := func(s string) {
		if s != "" {
			text = append(text, s)
		}
	}
	// "When f returns, x's memory is reclaimed".
	reclaimed := fmt.Sprintf("When %s returns, %s memory is reclaimed", l.fn, possessive(l.name()))
	switch {
	case l.kind == lkArena:
		reclaimed = fmt.Sprintf("When %s returns, the arena %s is gone", l.fn, l.name())
	case l.kind == lkObject && l.v != nil && refNoun(l.typ) != "":
		reclaimed = fmt.Sprintf("When %s returns, %s drops its reference, and the %s is freed if that was the last one", l.fn, l.v.Name(), refNoun(l.typ))
	case l.kind == lkObject && l.v != nil:
		reclaimed = fmt.Sprintf("When %s returns, %s is dropped and the object freed", l.fn, l.v.Name())
	case l.kind == lkObject:
		reclaimed = "At the end of the statement, the temporary is dropped and its object freed"
	case l.kind == lkStmtTemp:
		reclaimed = "At the end of the statement, the temporary is dropped"
	case !l.isVarLoc() && l.kind != lkClosure:
		reclaimed = fmt.Sprintf("When %s returns, its frame is reclaimed", l.fn)
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
		d.Msg = "cannot return " + x + a.viewPhrase(x, e.expr, l, q)
		if l.kind == lkBox && e.expr != nil && !isInterfaceExpr(a, e.expr) {
			d.Msg = fmt.Sprintf("cannot return %s as an interface: the interface would hold a copy of it in %s frame", x, possessive(l.fn))
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
			d.Msg = fmt.Sprintf("cannot return %s as an interface: an ordinary interface only borrows the box, which is dropped when %s returns", x, e.fn)
			if lendsRef {
				d.Msg = fmt.Sprintf("cannot return %s as an interface: an ordinary interface only borrows the %s, whose reference %s drops when it returns", x, ref, e.fn)
			}
		}
		switch {
		case l.kind == lkArena:
			add(sentence(about, fmt.Sprintf("%s, so %s would point at memory that is no longer allocated.", reclaimed, x)))
		case ref == "map":
			add(sentence(about, fmt.Sprintf("Converting %s to an ordinary interface lends it: the interface points at the map's table without a reference of its own.", x)))
		case ref == "channel":
			add(sentence(about, fmt.Sprintf("Converting %s to an ordinary interface lends it: the interface points at the channel without a reference of its own.", x)))
		case lends:
			add(sentence(about, fmt.Sprintf("Converting %s to an ordinary interface lends it: the interface points at the object, and %s still owns it.", x, x)))
		case l.kind == lkObject:
			add(sentence(about, fmt.Sprintf("%s, so %s would point at freed memory.", reclaimed, x)))
		case l.kind == lkStmtTemp:
			add(sentence(about, fmt.Sprintf("%s, before the caller gets %s, so it would point at a value that has been dropped.", reclaimed, x)))
		case l.isVarLoc():
			add(sentence(about, fmt.Sprintf("Its memory is reclaimed when %s returns, so %s would point at memory that no longer holds %s.", e.fn, x, l.v.Name())))
		case l.kind == lkClosure:
			add(sentence(about, fmt.Sprintf("That frame is reclaimed when %s returns.", e.fn)))
		default:
			add(sentence(about, fmt.Sprintf("That frame is reclaimed when %s returns, so %s would point at memory that no longer holds the value.", e.fn, pointer)))
		}
		if lendsRef {
			d.Fix = &diag.Fix{Intro: fmt.Sprintf("Return the %s itself, changing the result type to %s: the caller gets a reference of its own", ref, typeText(exprT, q)), Code: "return " + x}
		} else if lends {
			name := x
			d.Fix = &diag.Fix{Intro: fmt.Sprintf("Return the box itself, changing the result type to %s", typeText(a.types[e.expr].Type, q)), Code: "return " + name}
			d.More = []*diag.Fix{{Intro: "Or declare the interface as an owning interface, which takes ownership of what is converted into it (language spec section 18.5)", Code: "type I owner interface {\n\t...\n}"}}
		} else {
			a.returnFixes(d, e, l, q)
		}
	case sinkStore:
		d.Code = diag.StoreFrameView
		d.Msg = fmt.Sprintf("cannot store %s in %s, which outlives %s", x, e.dst, l.name())
		var why string
		switch e.where {
		case storeGlobal:
			why = fmt.Sprintf("%s, but package variables live for the whole program, so %s would be left pointing at reclaimed memory.", reclaimed, e.dst)
		case storeParam:
			why = fmt.Sprintf("%s, but %s is reached through %s, which points to memory that %s caller owns and that lives longer. It would be left pointing at reclaimed memory.", reclaimed, e.dst, e.via, possessive(e.fn))
		case storeHeap:
			why = fmt.Sprintf("%s, but %s may be on the heap and live longer, so it would be left pointing at reclaimed memory.", reclaimed, e.dst)
			if e.inMap != "" {
				why = fmt.Sprintf("%s, but a map keeps its keys and elements in a table on the heap, which every copy of the map shares and which may live longer, so it would be left pointing at reclaimed memory.", reclaimed)
			}
		case storeOuter:
			why = fmt.Sprintf("The function literal's frame is reclaimed each time the literal returns, but %s belongs to the function around it and lives longer.", e.dst)
		case storeStmt:
			why = fmt.Sprintf("%s, but %s lives longer, so it would be left pointing at a value that has been dropped.", reclaimed, e.dst)
		}
		add(sentence(about, why))
		a.storeFixes(d, e, l, q)
	case sinkCall:
		d.Code = diag.PassFrameView
		d.Msg = fmt.Sprintf("cannot pass %s to %s, which keeps it after returning", x, e.callee)
		add(sentence(about, fmt.Sprintf("%s %s, so %s would outlive %s.", e.callee, e.calleeWhy, pointer, l.name())))
		a.passFixes(d, e, l, q)
	case sinkGo:
		d.Code = diag.GoFrameView
		d.Msg = fmt.Sprintf("cannot pass %s to a task statement: the new task may outlive %s", x, l.name())
		add(sentence(about, fmt.Sprintf("The task started here runs on its own, and may still be running after %s returns and %s memory is reclaimed.", e.fn, possessive(l.name()))))
		a.handOffFixes(d, e, l, "task")
		if e.call != nil {
			d.More = append(d.More, &diag.Fix{Intro: fmt.Sprintf("Or start it with a sync.Scope, which waits for its tasks before its block ends, so that they may use %s variables", possessive(e.fn)),
				Code: fmt.Sprintf("var s sync.Scope\ns.Task(func() { %s })", exprText(e.call))})
		}
	case sinkSend:
		d.Code = diag.SendFrameView
		d.Msg = fmt.Sprintf("cannot send %s on %s: the receiver may outlive %s", x, exprText(e.ch), l.name())
		add(sentence(about, fmt.Sprintf("The value sent here may be received after %s returns and %s memory is reclaimed.", e.fn, possessive(l.name()))))
		a.handOffFixes(d, e, l, "send")
	case sinkLoop:
		d.Code = diag.LoopFrameView
		a.loopDiag(d, e, x, add)
	}
	d.Text = strings.Join(text, "\n")
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
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
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
		return "&" + l.v.Name()
	case lkClosure:
		return "the function literal"
	case lkResultN:
		return "the result"
	case lkVarargs:
		if call, ok := l.node.(*ast.CallExpr); ok {
			return "the variadic arguments of " + exprText(call.Fun)
		}
	}
	if l.node != nil {
		if e, ok := l.node.(ast.Expr); ok {
			return exprText(e)
		}
	}
	return "a pointer into " + possessive(l.fn) + " frame"
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
			fields = append(fields, v.Name()+" "+typeText(v.Type(), q))
		}
		d.Fix = &diag.Fix{
			Intro: "Return a value that carries its own state instead, such as a struct with a method",
			Code:  fmt.Sprintf("type state struct{ %s }\n\nfunc (s *state) Call() { ... } // uses s.%s", strings.Join(fields, "; "), l.captures[0].Name()),
		}
	case l.kind == lkMethodValue:
		d.Fix = &diag.Fix{Intro: "Return the receiver itself, and let the caller call the method on it."}
	case l.kind == lkBox:
		d.Fix = &diag.Fix{
			Intro: fmt.Sprintf("Return a pointer to memory that outlives %s, such as a field of a longer-lived value", e.fn),
			Code:  fmt.Sprintf("p.err = %s{...}\nreturn &p.err", typeText(l.typ, q)),
		}
		d.More = []*diag.Fix{{Intro: "or return a value that fits in one word, such as an error code."}}
	case l.kind == lkLit:
		T := typeText(l.typ, q)
		d.Fix = &diag.Fix{Intro: "To give the value to the caller, return it instead. This copies it, or moves it if it is an owner", Code: fmt.Sprintf("return %s{...}", T)}
		d.More = []*diag.Fix{{Intro: fmt.Sprintf("To keep it at a fixed address after %s returns, allocate it and return its owner, an owned[%s]", e.fn, T), Code: fmt.Sprintf("p := new(%s)\n...\nreturn p", T)}}
	case l.kind == lkSliceLit || (sliceOfVar(e.expr, l) && isArrayVar(l.v)):
		d.Fix = &diag.Fix{Intro: "To give the caller the elements, return an owner of them, such as a vec.Vec", Code: "v := vec.New[T]()\nv.Push(...)\nreturn v"}
		d.More = []*diag.Fix{{Intro: "or copy them into memory the caller passes in", Code: "func fill(dst []T) int {\n\t...\n\treturn copy(dst, buf[:n])\n}"}}
	case l.isVarLoc() && addrOfVar(e.expr, l):
		name := l.v.Name()
		d.Fix = &diag.Fix{Intro: "To give the value to the caller, return it instead. This copies it, or moves it if it is an owner", Code: "return " + name}
		d.More = []*diag.Fix{{Intro: fmt.Sprintf("To keep it at a fixed address after %s returns, allocate it and return its owner, an owned[%s]", e.fn, varType(l, q)), Code: fmt.Sprintf("%s := new(%s)\n...\nreturn %s", name, varType(l, q), name)}}
	case l.kind == lkObject && l.v != nil:
		name := l.v.Name()
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Return %s itself: an owned box moves to the caller, and a shared or synced one gives the caller its own reference", name), Code: "return " + name}
	case l.kind == lkObject:
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Return the box itself, changing the result type to %s", typeText(l.typ, q)), Code: "return " + exprText(l.node.(ast.Expr))}
	case l.kind == lkArena:
		arenaFixes(d, l)
	case l.kind == lkStmtTemp:
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Return the %s itself, which moves it to the caller, or a copy of what the view looks at.", typeText(l.typ, q))}
	case l.isVarLoc():
		name := l.v.Name()
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Return values instead of views of %s, or allocate %s and return its owner, an owned[%s]", name, name, varType(l, q)), Code: fmt.Sprintf("%s := new(%s)\n...\nreturn %s", name, varType(l, q), name)}
	default:
		d.Fix = &diag.Fix{Intro: "Return the value itself, which copies it, or allocate it so that it outlives the function."}
	}
}

func (a *analyzer) storeFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
	switch {
	case e.inMap != "":
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Store the value itself instead of a view of %s, changing the map's %s type to match.", l.name(), e.inMap)}
		d.More = []*diag.Fix{{Intro: "Or keep what the map refers to in memory that lives as long as the map, such as a package variable, or in a counted reference (shared[T] or synced[T]) stored in the map."}}
	case l.kind == lkClosure:
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Store a value that carries its own state instead of the function literal, such as a struct with a method, or make %s a package variable if it must outlive %s.", l.captureList(), l.fn)}
	case l.isVarLoc() && addrOfVar(e.expr, l) && !strings.Contains(trimAddr(e.dst), " "):
		name := l.v.Name()
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Store the value instead of a pointer to it, changing the type of %s to match. This copies %s, or moves it if it is an owner", trimAddr(e.dst), name), Code: fmt.Sprintf("%s = %s", trimAddr(e.dst), name)}
		d.More = []*diag.Fix{{Intro: fmt.Sprintf("Or allocate %s and store its owner, an owned[%s], which moves into %s", name, varType(l, q), trimAddr(e.dst)), Code: fmt.Sprintf("%s = new(%s)", trimAddr(e.dst), varType(l, q))}}
	case l.kind == lkArena:
		arenaFixes(d, l)
	case l.kind == lkStmtTemp:
		d.Fix = stmtTempFix(l)
	case l.kind == lkObject && l.v != nil && refNoun(l.typ) != "":
		name := l.v.Name()
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Store %s itself, changing the type of %s to %s: it then counts a reference of its own", name, trimAddr(e.dst), typeText(l.typ, q)), Code: fmt.Sprintf("%s = %s", trimAddr(e.dst), name)}
	case l.kind == lkObject && l.v != nil:
		name := l.v.Name()
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Store %s itself, changing the type of %s to match: an owned box moves there, and a shared or synced one is counted", name, trimAddr(e.dst)), Code: fmt.Sprintf("%s = %s", trimAddr(e.dst), name)}
	case l.isVarLoc():
		name := l.v.Name()
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Store values instead of views of %s, or allocate %s and store its owner, an owned[%s], which moves there", name, name, varType(l, q)), Code: fmt.Sprintf("%s = new(%s)", trimAddr(e.dst), varType(l, q))}
	default:
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Store the value itself instead of a view of %s frame, or allocate it so that it lives as long as %s.", possessive(l.fn), e.dst)}
	}
}

// stmtTempFix is the fix for a view of an owner temporary that outlives
// its statement: keep the owner in a variable, which lives until the end
// of its scope.
func stmtTempFix(l *loc) *diag.Fix {
	return &diag.Fix{
		Intro: "Keep the owner in a variable, so that it lives until the end of its scope, and call the method on the variable",
		Code:  fmt.Sprintf("v := %s\n... v.%s() ...", exprText(l.node.(ast.Expr)), l.method),
	}
}

// trimAddr turns a message's destination back into an expression:
// "package variable G" into "G".
func trimAddr(dst string) string {
	return strings.TrimPrefix(dst, "package variable ")
}

func (a *analyzer) passFixes(d *diag.Diagnostic, e *viewErr, l *loc, q types2.Qualifier) {
	if l.isVarLoc() {
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("If %s should keep its own copy of %s, change it to take the value, not a pointer, or an owner it can keep: an owned[%s] from new, a shared[%s], or a library owner such as a vec.Vec.", e.callee, l.v.Name(), varType(l, q), varType(l, q))}
		return
	}
	d.Fix = &diag.Fix{Intro: fmt.Sprintf("Change %s to take a value or an owner, which it can keep: an owned[T] from new, a shared[T], or a library owner such as a vec.Vec.", e.callee)}
}

func (a *analyzer) handOffFixes(d *diag.Diagnostic, e *viewErr, l *loc, how string) {
	if !addrOfVar(e.expr, l) {
		d.Fix = &diag.Fix{Intro: "Pass a value, which is copied, or an owner, which moves, instead of a view of the frame."}
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
					args = append(args, val)
				} else {
					args = append(args, exprText(arg))
				}
			}
			code = fmt.Sprintf("task %s(%s)", exprText(e.call.Fun), strings.Join(args, ", "))
		}
	case "send":
		code = fmt.Sprintf("%s <- %s", exprText(e.ch), val)
	}
	if code == "" {
		d.Fix = &diag.Fix{Intro: "Pass the value itself. This copies it, or moves it if it is an owner."}
		return
	}
	d.Fix = &diag.Fix{Intro: "Pass the value itself. This copies it, or moves it if it is an owner", Code: code}
}

// loopDiag explains a view of a loop's storage kept past the iteration. The
// view is kept either by a store in the function itself (e.dst says where)
// or by a callee (e.callee and e.calleeWhy say what it does).
func (a *analyzer) loopDiag(d *diag.Diagnostic, e *viewErr, x string, add func(string)) {
	l, q := e.l, e.qual
	line := a.line(e.loop.Pos())
	// head starts the message and holder is what would see the next
	// iteration's value. keeps is the callee's own sentence, if a callee
	// keeps the view.
	head := fmt.Sprintf("cannot keep %s in %s after this iteration", x, e.dst)
	holder := e.dst
	var keeps string
	if e.callee != "" {
		head = fmt.Sprintf("cannot pass %s to %s, which keeps it after this iteration", x, e.callee)
		holder = "what " + e.callee + " kept"
		keeps = fmt.Sprintf("%s %s.", e.callee, e.calleeWhy)
	}
	if v := e.iterVar; l.kind == lkClosure && v != nil {
		name, T := v.Name(), typeText(v.Type(), q)
		kept := "the literal kept in " + e.dst
		if e.callee != "" {
			kept = "the literal kept by " + e.callee
		}
		d.Msg = fmt.Sprintf("%s: it uses %s, which the loop at line %d reuses in every iteration", head, name, line)
		add(sentence(
			fmt.Sprintf("A function literal refers to the variables it uses where they live, and %s lives in %s frame, where every iteration of the loop at line %d reuses the same memory for it.", name, possessive(l.fn), line),
			keeps,
			fmt.Sprintf("When the next iteration starts, %s would see that iteration's %s, not this one's.", kept, name)))
		saved := fmt.Sprintf("var saved %s // declared before the loop\n...\nsaved = %s", T, name)
		if e.callee != "" {
			d.Fix = &diag.Fix{Intro: fmt.Sprintf("If %s needs the function literal only during the call, change it so it does not keep it. To use %s later, keep its value instead of the function literal", e.callee, name), Code: saved}
			d.More = []*diag.Fix{{Intro: fmt.Sprintf("Or have the function literal take %s as a parameter, and pass it where the literal is called.", name)}}
			return
		}
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("If the function literal is needed only in this iteration, call it before the iteration ends. To use %s later, keep its value instead of the function literal", name), Code: saved}
		d.More = []*diag.Fix{{Intro: fmt.Sprintf("Or pass %s to the function literal as a parameter when it is called", name), Code: fmt.Sprintf("%s = func(%s %s) { ... }\n...\n%s(%s)", e.dst, name, T, e.dst, name)}}
		return
	}
	if l.isVarLoc() {
		name := l.v.Name()
		d.Msg = fmt.Sprintf("%s: the loop at line %d reuses %s in every iteration", head, line, name)
		add(sentence(a.subject(x, e.expr, l, q),
			fmt.Sprintf("%s lives in %s frame, where every iteration of the loop at line %d reuses the same memory for it.", name, possessive(l.fn), line),
			keeps,
			fmt.Sprintf("When the next iteration starts, %s would see that iteration's %s, not this one's.", holder, name)))
		saved := fmt.Sprintf("var saved %s // declared before the loop\n...\nsaved = %s", varType(l, q), name)
		if e.callee != "" {
			d.Fix = &diag.Fix{Intro: fmt.Sprintf("If %s needs the view only during the call, change it so it does not keep it. To use the value later, keep the value instead of a view of it", e.callee), Code: saved}
			return
		}
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("If %s is needed only in this iteration, use it before the iteration ends. To use the value later, keep the value instead of a view of it", e.dst), Code: saved}
		return
	}
	d.Msg = fmt.Sprintf("%s: the loop at line %d makes it in the same memory in every iteration", head, line)
	add(a.intro(l, q))
	add(sentence(fmt.Sprintf("Every iteration of the loop at line %d reuses that memory.", line), keeps,
		fmt.Sprintf("When the next iteration starts, %s would see that iteration's value, not this one's.", holder)))
	if e.callee != "" {
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Change %s so it does not keep the view, or pass a copy of the value instead of a view of it.", e.callee)}
		return
	}
	d.Fix = &diag.Fix{Intro: fmt.Sprintf("Use %s before the iteration ends, or keep a copy of the value instead of a view of it.", x)}
}

// arenaFixes are the fixes for memory from a local arena that would outlive
// it.
func arenaFixes(d *diag.Diagnostic, l *loc) {
	d.Fix = &diag.Fix{
		Intro: "Allocate what must outlive the arena from an allocator that does, passed explicitly",
		Code:  "heap := alloc.Default() // before alloc.Use\n...\nv := vec.New[T](heap)",
	}
	d.More = []*diag.Fix{{Intro: "Or install an arena that outlives the result, such as one the caller passes in, or make the copy that must outlive the arena after the Restore is dropped."}}
}
