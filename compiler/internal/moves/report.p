// Translated by go2pu from internal/moves/report.go at the go-cutover tag; edited by hand since.

package moves

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// Messages (language spec section 21.1). Each says what happened, explains
// the rule with the program's names and the line of the move, and shows
// fixes as code.

func (c *checker) qualifier() types2.Qualifier { return types2.RelativeTo(c.pkg) }

func (c *checker) typeName(T types2.Type) string { return types2.TypeString(T, c.qualifier()) }

func (c *checker) line(p token.Pos) int { return c.fset.Position(p).Line }

func (c *checker) newDiag(at token.Pos, code diag.Code, format string, args ...any) *diag.Diagnostic {
	return diag.New(c.fset.Position(at), code, format, args...)
}

// exprText writes an expression for a message.
func exprText(e ast.Expr) string {
	if e == nil {
		return ""
	}
	if _, ok := ast.Unparen(e).(*ast.FuncLit); ok {
		return "the function literal"
	}
	return strings.ReplaceAll(types2.ExprString(e), "…", "...")
}

// hasClone reports whether T or *T has a Clone method.
func hasClone(T types2.Type) bool {
	for _, t := range []types2.Type{T, types2.NewPointer(T)} {
		if types2.NewMethodSet(t).Lookup(nil, "Clone") != nil {
			return true
		}
		// Lookup with a nil package finds only exported methods, which
		// Clone is.
	}
	return false
}

// ownerIntro says what makes T an owner.
func (c *checker) ownerIntro(T types2.Type) string {
	return compat.Sprintfb("%s is an owner type: its values own what they hold and release it when they are dropped.", compat.ArgS(c.typeName(T)))
}

// howMoved says how a value left a variable: "moved to n", "moved into
// consume (its parameter takes a Buf by value)".
func (c *checker) howMoved(mv *move) string {
	switch mv.kind {
	case mvAssign:
		return compat.Concat("moved to ", mv.to)
	case mvArg:
		return compat.Sprintfb("moved into %s (its parameter takes %s by value)", compat.ArgS(mv.to), compat.ArgS(article(c.typeName(mv.typ))))
	case mvRecv:
		name := mv.to
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = strings.SliceFrom(name, i+1)
		}
		return compat.Sprintfb("moved by the call %s() (%s has a value receiver)", compat.ArgS(mv.to), compat.ArgS(name))
	case mvReturn:
		return "returned"
	case mvSend:
		return compat.Concat("moved by the send on ", mv.to)
	case mvGo:
		return "moved to the task started"
	case mvGoRecv:
		return compat.Concat("moved to the task started by task ", mv.to, "()")
	case mvTask:
		return compat.Concat("taken by the task started by ", mv.to)
	case mvStore:
		return compat.Concat("moved into ", mv.to)
	case mvDrop:
		return compat.Concat("dropped by ", mv.to, "()")
	case mvConv:
		return compat.Concat("converted to ", mv.to)
	case mvAssert:
		return compat.Concat("moved out by the type assertion ", mv.to)
	case mvNewValue:
		return "given a new value"
	case mvTake:
		return compat.Concat("emptied by ", mv.to)
	case mvScopeEnd:
		verb := "dropped"
		if mv.typ != nil && !bounds(mv.typ) {
			verb = "released"
		}
		if mv.to != "" {
			return compat.Concat(verb, " when ", mv.to, " left its block")
		}
		return compat.Concat(verb, " at the end of its block")
	case mvReclaim:
		return compat.Concat("reclaimed by ", mv.to)
	case mvFree:
		return compat.Concat("passed to ", mv.to)
	}
	return "moved"
}

// deferUseDiag explains an owner that a deferred call uses when the function
// returns (a function literal it calls or is given, a function variable
// holding one, or a view of the owner in its arguments), after the owner was
// moved or dropped.
func (c *checker) deferUseDiag(v *types2.Var, s vstate) *diag.Diagnostic {
	d := c.useDiag(s.dl.use, v, s)
	name, at := v.Name(), c.line(s.dl.stmt)
	if s.st == maybe && s.mv != nil && s.mv.loop {
		return d
	}
	mvLine := c.line(s.mv.pos)
	how := c.howMoved(s.mv)
	what, runs := "the deferred function", ""
	switch s.dl.kind {
	case deferView:
		what = "the deferred call"
		runs = compat.Sprintfb(" The call deferred at line %d keeps a view of %s in its receiver or arguments and runs when the function returns, after line %d, so it would look at what %s no longer holds.", compat.ArgI(int64(at)), compat.ArgS(name), compat.ArgI(int64(mvLine)), compat.ArgS(name))
	case deferStored:
		runs = compat.Sprintfb(" The function %s, which holds a function literal that uses %s, is deferred at line %d and runs when the function returns, after line %d, so it would use what %s no longer holds.", compat.ArgS(s.dl.via), compat.ArgS(name), compat.ArgI(int64(at)), compat.ArgI(int64(mvLine)), compat.ArgS(name))
	default:
		runs = compat.Sprintfb(" The function literal deferred at line %d runs when the function returns, after line %d, so it would use what %s no longer holds.", compat.ArgI(int64(at)), compat.ArgI(int64(mvLine)), compat.ArgS(name))
	}
	if s.st == maybe {
		d.Msg = compat.Sprintfb("%s may have been %s at line %d, but %s (line %d) uses it when the function returns", compat.ArgS(name), compat.ArgS(how), compat.ArgI(int64(mvLine)), compat.ArgS(what), compat.ArgI(int64(at)))
	} else {
		d.Msg = compat.Sprintfb("%s was %s at line %d, but %s (line %d) uses it when the function returns", compat.ArgS(name), compat.ArgS(how), compat.ArgI(int64(mvLine)), compat.ArgS(what), compat.ArgI(int64(at)))
	}
	d.Text = compat.Concat(d.Text, runs)
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Give %s a new value after line %d, so that the deferred call finds one", compat.ArgS(name), compat.ArgI(int64(mvLine))), Code: compat.Sprintfb("%s = %s", compat.ArgS(name), compat.ArgS(c.newValue(v.Type())))})
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or do the work of the deferred call at the end of the function, before line %d, instead of deferring it", compat.ArgI(int64(mvLine)))})})
	return d
}

// staleDiag explains a read of e, part of variable v, that may hold a view
// w whose owner has lost the value it looked at (language spec section
// 18.3).
func (c *checker) staleDiag(e ast.Expr, v *types2.Var, w view) *diag.Diagnostic {
	name, owner := exprText(e), w.of.Name()
	made, at := c.line(w.pos), c.line(w.stale.pos)
	how := c.howMoved(w.stale)
	var d *diag.Diagnostic
	if w.maybe {
		d = c.newDiag(e.Pos(), diag.StaleView, "%s holds a view of %s (%s, line %d), which may have been %s at line %d, so %s cannot be used here", pu.Box(name), pu.Box(owner), pu.Box(w.text), made, pu.Box(how), at, pu.Box(name))
	} else {
		d = c.newDiag(e.Pos(), diag.StaleView, "%s holds a view of %s (%s, line %d), which was %s at line %d, so %s can no longer be used here", pu.Box(name), pu.Box(owner), pu.Box(w.text), made, pu.Box(how), at, pu.Box(name))
	}
	holder := name
	if name != v.Name() {
		holder = compat.Concat(name, ", part of ", v.Name(), ",")
	}
	lost := compat.Sprintfb("At line %d %s was %s", compat.ArgI(int64(at)), compat.ArgS(owner), compat.ArgS(how))
	if w.maybe {
		lost = compat.Sprintfb("At line %d %s was %s on some paths to this point", compat.ArgI(int64(at)), compat.ArgS(owner), compat.ArgS(how))
	}
	owns, rule := "owns", "A view of an owner held in a local variable may be used only while the variable keeps the value the view was made from."
	if isCountedBox(w.of.Type()) {
		owns, rule = "refers to", "A view obtained through a counted reference held in a local variable may be used only while the variable keeps the reference the view was made from."
	} else if !isOwner(w.of.Type()) {
		owns, rule = "holds", "A view of a local variable whose type has drop glue may be used only until the variable's scope ends, where the variable is released."
	} else if w.stale.kind == mvReclaim {
		rule = "A view of a local arena's memory may be used only until Reset or Release reclaims it."
	}
	if w.stale.kind == mvFree {
		rule = compat.Concat(rule, " A call that may free memory the view looks at, or move an owner out of it, through a pointer to the variable or to memory it owns ends the view as well.")
	}
	d.Text = compat.Sprintfb("%s looks at memory that %s %s, without owning it: the view was made by %s at line %d. %s %s, so the view may look at memory that was released, or at a value that now belongs to something else.", compat.ArgS(holder), compat.ArgS(owner), compat.ArgS(owns), compat.ArgS(w.text), compat.ArgI(int64(made)), compat.ArgS(rule), compat.ArgS(lost))
	switch w.stale.kind {
	case mvNewValue:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Use %s before line %d, or give %s its new value only after the last use of %s", compat.ArgS(name), compat.ArgI(int64(at)), compat.ArgS(owner), compat.ArgS(name))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or make the view again after line %d", compat.ArgI(int64(at))), Code: compat.Sprintfb("%s = %s", compat.ArgS(name), compat.ArgS(w.text))})})
	case mvScopeEnd:
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Declare %s in a block that encloses every use of %s, or use %s only inside the block of %s", compat.ArgS(owner), compat.ArgS(name), compat.ArgS(name), compat.ArgS(owner))})
	default:
		act := compat.Concat("move ", owner)
		switch w.stale.kind {
		case mvDrop:
			act = compat.Concat("drop ", owner)
		case mvTake:
			act = compat.Concat("empty ", owner)
		case mvReclaim:
			act = compat.Concat("call ", w.stale.to)
		case mvFree:
			act = "make that call"
		}
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Use %s before line %d, or %s only after the last use of %s", compat.ArgS(name), compat.ArgI(int64(at)), compat.ArgS(act), compat.ArgS(name))})
		if hasClone(w.of.Type()) && w.stale.kind != mvDrop && w.stale.kind != mvTake && w.stale.kind != mvReclaim && w.stale.kind != mvFree {
			d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: compat.Sprintfb("Or move a copy at line %d, so that %s keeps its value", compat.ArgI(int64(at)), compat.ArgS(owner)), Code: compat.Concat(owner, ".Clone()")})})
		}
	}
	return d
}

// lostVerb says how an owner loses its value, for a sentence about it: "is
// moved into consume (...)".
func (c *checker) lostVerb(mv move) string {
	return compat.Concat("is ", c.howMoved(&mv))
}

// pinnedDiag explains moving, dropping or replacing the value of v while a
// view of it is kept where the move checker cannot follow it.
func (c *checker) pinnedDiag(v *types2.Var, p *pinMark, mv move) *diag.Diagnostic {
	owner := v.Name()
	d := c.newDiag(mv.pos, diag.LostView, "%s %s here, but a view of it (%s, line %d) was %s at line %d, where the compiler cannot check its uses", pu.Box(owner), pu.Box(c.lostVerb(mv)), pu.Box(p.text), c.line(p.pos), pu.Box(p.how), c.line(p.where))
	rule := "A view of an owner held in a local variable may be used only while the variable keeps the value the view was made from."
	if !bounds(v.Type()) {
		rule = "A view of a local variable whose type has drop glue may be used only until the variable's scope ends, where the variable is released."
	}
	d.Text = compat.Sprintfb("%s The compiler follows views through local variables and their fields, and into the arguments of the calls that store them there, but the view of %s made by %s at line %d went into memory it does not follow, so it cannot check that the view is not used after line %d.", compat.ArgS(rule), compat.ArgS(owner), compat.ArgS(p.text), compat.ArgI(int64(c.line(p.pos))), compat.ArgI(int64(c.line(mv.pos))))
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Keep the view in a local variable or a field of one, where the compiler follows it, or do not %s while the view is kept", compat.ArgS(strings.TrimPrefix(c.lostVerbBase(mv), "is ")))})
	return d
}

// heapPinnedDiag explains moving v to another task while a view of what it
// holds on the heap may be kept by a call.
func (c *checker) heapPinnedDiag(v *types2.Var, p *pinMark, mv move) *diag.Diagnostic {
	owner := v.Name()
	d := c.newDiag(mv.pos, diag.LostView, "%s %s here, but a view of what it holds (%s, line %d) was %s at line %d, where the compiler cannot check its uses", pu.Box(owner), pu.Box(c.lostVerb(mv)), pu.Box(p.text), c.line(p.pos), pu.Box(p.how), c.line(p.where))
	d.Text = compat.Sprintfb("An owner may move to another task only when no other alias of the memory it holds is left behind (language spec section 8.8). The view of %s made by %s at line %d went into memory the compiler does not follow, so this task could still use it while the task that receives %s does.", compat.ArgS(owner), compat.ArgS(p.text), compat.ArgI(int64(c.line(p.pos))), compat.ArgS(owner))
	d.Fix = pu.New(diag.Fix{Intro: "Keep the view in a local variable or a field of one, where the compiler follows it, or do not hand it to a call that keeps it"})
	return d
}

// lostVerbBase says what the program does to an owner, for a fix: "move
// c", "give c a new value".
func (c *checker) lostVerbBase(mv move) string {
	switch mv.kind {
	case mvNewValue:
		return "give it a new value"
	case mvScopeEnd:
		return "let its block end"
	case mvDrop:
		return "drop it"
	case mvTake:
		return "empty it with take"
	case mvReclaim:
		return "reclaim its memory"
	case mvFree:
		return "make that call"
	}
	return "move it"
}

// newValue writes a value of type T for a fix that gives a moved-from
// variable a new one: a composite literal, or a call that makes a box or an
// interface value.
func (c *checker) newValue(T types2.Type) string {
	switch u := T.Underlying().(type) {
	case *types2.Interface:
		return "..." // whatever new value the variable should hold
	case *types2.Wrapper:
		if u.IsBox() {
			return compat.Concat("new(", c.typeName(u.Elem()), ")")
		}
	case *types2.TaggedUnion:
		if u.NumVariants() > 0 {
			v := compat.Concat(c.typeName(T), ".", u.Variant(0).Name())
			if u.Variant(0).HasPayload() {
				v = compat.Concat(v, "(...)")
			}
			return v
		}
	case *types2.Basic:
		return compat.Concat(c.typeName(T), "(0)")
	}
	return compat.Concat(c.typeName(T), "{}")
}

func article(s string) string {
	if s != "" && strings.ContainsRune("aeiouAEIOU", rune(s[0])) {
		return compat.Concat("an ", s)
	}
	return compat.Concat("a ", s)
}

// useDiag explains a use of v where it holds nothing.
func (c *checker) useDiag(id *ast.Ident, v *types2.Var, s vstate) *diag.Diagnostic {
	name, T := v.Name(), v.Type()
	mv := s.mv
	if mv == nil {
		mv = &move{pos: id.Pos()}
	}
	at := c.line(mv.pos)
	if s.st == maybe && mv.loop {
		d := c.newDiag(id.Pos(), diag.UseMaybeMoved, "%s was %s at line %d in the previous iteration of this loop", pu.Box(name), pu.Box(c.howMoved(mv)), at)
		d.Text = compat.Sprintfb("%s %s is declared outside the loop, and the loop moves its value away at line %d without giving it a new one, so when the next iteration starts %s holds nothing.", compat.ArgS(c.ownerIntro(T)), compat.ArgS(name), compat.ArgI(int64(at)), compat.ArgS(name))
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Declare %s inside the loop, so that each iteration has its own", compat.ArgS(name)), Code: compat.Sprintfb("for ... {\n\tvar %s %s\n\t...\n}", compat.ArgS(name), compat.ArgS(c.typeName(T)))})
		more := pu.New(diag.Fix{Intro: compat.Sprintfb("Or give %s a new value after line %d", compat.ArgS(name), compat.ArgI(int64(at))), Code: compat.Sprintfb("%s = %s", compat.ArgS(name), compat.ArgS(c.newValue(T)))})
		d.More = compat.Clone([]*diag.Fix{more})
		if hasClone(T) {
			d.More = compat.Append(d.More, pu.New(diag.Fix{Intro: compat.Sprintfb("Or move a copy at line %d", compat.ArgI(int64(at))), Code: compat.Concat(name, ".Clone()")}))
		}
		return d
	}
	if s.st == maybe {
		d := c.newDiag(id.Pos(), diag.UseMaybeMoved, "%s may have been moved at line %d, so it cannot be used here", pu.Box(name), at)
		d.Text = compat.Sprintfb("%s %s is %s at line %d on some paths to this point and not on others. On those paths %s holds nothing afterwards, and the compiler cannot let a value be used where it may already belong to someone else.", compat.ArgS(c.ownerIntro(T)), compat.ArgS(name), compat.ArgS(c.howMoved(mv)), compat.ArgI(int64(at)), compat.ArgS(name))
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Use %s before line %d, or give it a new value on the path that moves it", compat.ArgS(name), compat.ArgI(int64(at))), Code: compat.Sprintfb("%s = %s", compat.ArgS(name), compat.ArgS(c.newValue(T)))})
		return d
	}
	d := c.newDiag(id.Pos(), diag.UseAfterMove, "%s was %s at line %d, so it can no longer be used here", pu.Box(name), pu.Box(c.howMoved(mv)), at)
	what := "Assigning an owner moves it"
	switch mv.kind {
	case mvArg, mvGo:
		what = "Passing an owner by value moves it"
	case mvGoRecv:
		what = "Starting a method of an owned box with a task statement moves the box into the new task, which drops it when the method returns,"
	case mvRecv:
		what = "Calling a method with a value receiver moves the receiver into the method"
	case mvSend:
		what = "Sending an owner moves it"
	case mvStore:
		what = "Storing an owner moves it"
	case mvDrop:
		what = "Calling Drop ends an owner's life"
	case mvReturn:
		what = "Returning an owner moves it to the caller"
	}
	holder := "it"
	if mv.kind == mvAssign || mv.kind == mvStore {
		holder = mv.to
	}
	text := compat.Sprintfb("%s %s instead of copying it, so that what it owns has exactly one owner.", compat.ArgS(c.ownerIntro(T)), compat.ArgS(what))
	if mv.kind == mvDrop {
		text = compat.Sprintfb("%s %s: what %s owned has been released.", compat.ArgS(c.ownerIntro(T)), compat.ArgS(what), compat.ArgS(name))
	} else if holder != "it" {
		text = compat.Concat(text, compat.Sprintfb(" Since line %d, %s owns it and %s holds nothing.", compat.ArgI(int64(at)), compat.ArgS(holder), compat.ArgS(name)))
	} else {
		text = compat.Concat(text, compat.Sprintfb(" Since line %d, %s holds nothing.", compat.ArgI(int64(at)), compat.ArgS(name)))
	}
	d.Text = text
	var fixes []*diag.Fix
	switch mv.kind {
	case mvAssign:
		if hasClone(T) {
			fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: compat.Sprintfb("If you need two independent values, copy it explicitly at line %d", compat.ArgI(int64(at))), Code: compat.Sprintfb("%s := %s.Clone()", compat.ArgS(mv.to), compat.ArgS(name))}))
		}
		fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: compat.Sprintfb("If %s only needs to look at the value, give it a pointer instead", compat.ArgS(mv.to)), Code: compat.Sprintfb("%s := &%s", compat.ArgS(mv.to), compat.ArgS(name))}))
	case mvArg:
		fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: compat.Sprintfb("If %s only needs to look at the value, change its parameter to *%s and pass a pointer", compat.ArgS(mv.to), compat.ArgS(c.typeName(T))), Code: compat.Sprintfb("%s(&%s)", compat.ArgS(mv.to), compat.ArgS(name))}))
		if hasClone(T) {
			fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: "If both need a value, pass a copy", Code: compat.Sprintfb("%s(%s.Clone())", compat.ArgS(mv.to), compat.ArgS(name))}))
		}
	case mvRecv:
		method := mv.to
		if i := strings.LastIndexByte(method, '.'); i >= 0 {
			method = strings.SliceFrom(method, i+1)
		}
		fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: compat.Sprintfb("Declare %s with a pointer receiver so that calling it does not consume %s", compat.ArgS(method), compat.ArgS(name)), Code: compat.Sprintfb("func (x *%s) %s(...)", compat.ArgS(c.typeName(T)), compat.ArgS(method))}))
	case mvGoRecv:
		fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: compat.Sprintfb("To keep using %s while the task runs, make it a synced box: the task then takes a reference of its own", compat.ArgS(name)), Code: compat.Sprintfb("%s := alloc.Synced[%s]()", compat.ArgS(name), compat.ArgS(boxElem(c, T)))}))
	default:
		if hasClone(T) {
			fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: compat.Sprintfb("If both need a value, move a copy at line %d", compat.ArgI(int64(at))), Code: compat.Concat(name, ".Clone()")}))
		}
	}
	fixes = compat.Append(fixes, pu.New(diag.Fix{Intro: compat.Sprintfb("If this is the last use of %s, move it before line %d, where %s still holds its value.", compat.ArgS(name), compat.ArgI(int64(at)), compat.ArgS(name))}))
	d.Fix, d.More = fixes[0], fixes[1:]
	return d
}

// capturedDiag explains a move of a captured variable inside a literal.
func (c *checker) capturedDiag(id *ast.Ident, v *types2.Var) *diag.Diagnostic {
	name := v.Name()
	d := c.newDiag(id.Pos(), diag.MoveCaptured, "cannot move %s inside a function literal: the literal may run more than once, or never", pu.Box(name))
	d.Text = compat.Sprintfb("%s is a variable of the function around the literal (declared at %s), which the literal uses where it lives. Moving it here would leave %s empty after the first call, and the function could not tell whether %s still needs to be dropped.", compat.ArgS(name), compat.ArgS(c.basePos(v.Pos())), compat.ArgS(name), compat.ArgS(name))
	d.Fix = pu.New(diag.Fix{Intro: "Pass the value to the literal as an argument", Code: compat.Sprintfb("do := func(%s %s) { ... }\ndo(%s)", compat.ArgS(name), compat.ArgS(c.typeName(v.Type())), compat.ArgS(name))})
	return d
}

func (c *checker) basePos(p token.Pos) string {
	pp := c.fset.Position(p)
	f := pp.Filename
	if i := strings.LastIndexByte(f, '/'); i >= 0 {
		f = strings.SliceFrom(f, i+1)
	}
	return compat.Sprintfb("%s:%d", compat.ArgS(f), compat.ArgI(int64(pp.Line)))
}

// placeDiag explains a move out of something other than a variable.
func (c *checker) placeDiag(e ast.Expr, k placeKind) *diag.Diagnostic {
	x := exprText(e)
	T := c.typeOf(e)
	root := x
	var first string
	switch k {
	case placeField:
		if se, ok := ast.Unparen(e).(*ast.SelectorExpr); ok {
			root = exprText(se.X)
		}
		first = compat.Sprintfb("cannot move %s out of %s: %s is a field, not a variable", compat.ArgS(x), compat.ArgS(root), compat.ArgS(x))
	case placeEmbedded:
		se := ast.Unparen(e).(*ast.SelectorExpr)
		root = exprText(se.X)
		first = compat.Sprintfb("cannot call %s: its value receiver would move an embedded field out of %s", compat.ArgS(x), compat.ArgS(root))
	case placeElem:
		if ix, ok := ast.Unparen(e).(*ast.IndexExpr); ok {
			root = exprText(ix.X)
		}
		first = compat.Sprintfb("cannot move %s out of %s: %s is an element, not a variable", compat.ArgS(x), compat.ArgS(root), compat.ArgS(x))
	case placePointer:
		if st, ok := ast.Unparen(e).(*ast.StarExpr); ok {
			root = exprText(st.X)
		}
		first = compat.Sprintfb("cannot move %s out of the value %s points to", compat.ArgS(x), compat.ArgS(root))
	case placeGlobal:
		first = compat.Sprintfb("cannot move package variable %s: only a local variable can be moved out of", compat.ArgS(x))
	}
	d := c.newDiag(e.Pos(), diag.MoveOutOfPlace, "%s", pu.Box(first))
	tn := "the value"
	if T != nil {
		tn = c.typeName(T)
	}
	switch k {
	case placeGlobal:
		d.Text = compat.Sprintfb("%s is an owner type, so using %s here would move it. But a package variable lives for the whole program and would still believe it holds the value.", compat.ArgS(tn), compat.ArgS(x))
	case placePointer:
		d.Text = compat.Sprintfb("%s is an owner type, so using %s here would move it. But the value belongs to whatever %s points into, which would free it again when it is dropped.", compat.ArgS(tn), compat.ArgS(x), compat.ArgS(root))
	default:
		d.Text = compat.Sprintfb("%s is an owner type, so taking %s would move it. But %s would still believe it owns %s and would free it again when %s is dropped.", compat.ArgS(tn), compat.ArgS(x), compat.ArgS(root), compat.ArgS(x), compat.ArgS(root))
	}
	if k == placeEmbedded {
		d.Fix = pu.New(diag.Fix{Intro: "Declare the method with a pointer receiver, so that calling it borrows the field instead of moving it."})
		return d
	}
	d.Fix = pu.New(diag.Fix{Intro: "To move it out and leave an empty value in its place", Code: compat.Sprintfb("v := take(&%s)", compat.ArgS(x))})
	if k == placePointer {
		d.Fix.Code = compat.Sprintfb("v := take(%s)", compat.ArgS(root))
	}
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "To look at it without moving it", Code: compat.Sprintfb("v := &%s", compat.ArgS(x))})})
	if k == placePointer {
		d.More[0].Code = compat.Sprintfb("v := %s", compat.ArgS(root))
	}
	if T != nil && hasClone(T) {
		d.More = compat.Append(d.More, pu.New(diag.Fix{Intro: "To copy it", Code: compat.Sprintfb("v := %s.Clone()", compat.ArgS(x))}))
	}
	return d
}

// assertPlaceDiag explains a moving assertion to typ, or a type switch
// with a binding, on a field, an element or *p.
func (c *checker) assertPlaceDiag(e ast.Expr, k placeKind, typ string, isSwitch bool) *diag.Diagnostic {
	x := exprText(e)
	root, what, taken := x, "a field", compat.Concat("take(&", x, ")")
	switch p := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		root = exprText(p.X)
	case *ast.IndexExpr:
		root, what = exprText(p.X), "an element"
	case *ast.StarExpr:
		root, taken = exprText(p.X), compat.Concat("take(", exprText(p.X), ")")
	}
	how := compat.Concat("the assertion ", x, ".(", typ, ")")
	subject := compat.Concat("The assertion ", x, ".(", typ, ")")
	code := compat.Concat("v := ", taken, ".(", typ, ")")
	if isSwitch {
		how = compat.Concat("switch v := ", x, ".(type)")
		subject = compat.Concat("A type switch that declares a variable, such as ", how, ",")
		code = compat.Concat("switch v := ", taken, ".(type) {")
	}
	var d *diag.Diagnostic
	if k == placePointer {
		d = c.newDiag(e.Pos(), diag.MoveOutOfPlace, "cannot move %s out of the value %s points to with %s", pu.Box(x), pu.Box(root), pu.Box(how))
		d.Text = compat.Sprintfb("%s moves the value out of %s. But the value belongs to whatever %s points into, which would drop it again when it is dropped. Only a whole variable or a temporary can be asserted out of.", compat.ArgS(subject), compat.ArgS(x), compat.ArgS(root))
	} else {
		d = c.newDiag(e.Pos(), diag.MoveOutOfPlace, "cannot move %s out of %s with %s: %s is %s, not a variable", pu.Box(x), pu.Box(root), pu.Box(how), pu.Box(x), pu.Box(what))
		d.Text = compat.Sprintfb("%s moves the value out of %s. But %s would still believe it holds the value and would drop it again when %s is dropped. Only a whole variable or a temporary can be asserted out of.", compat.ArgS(subject), compat.ArgS(x), compat.ArgS(root), compat.ArgS(root))
	}
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Take the value out first, which leaves the zero value in %s, and put back what you keep", compat.ArgS(x)), Code: code})
	return d
}

// rangeDiag explains a range loop that would copy owner elements.
func (c *checker) rangeDiag(st *ast.RangeStmt, elem types2.Type) *diag.Diagnostic {
	x := exprText(st.X)
	d := c.newDiag(st.Value.Pos(), diag.RangeCopiesOwner, "cannot copy the elements of %s in a range loop: %s is an owner type", pu.Box(x), pu.Box(c.typeName(elem)))
	d.Text = compat.Sprintfb("A range loop copies each element into %s. Copying an owner would leave two values that both release what it owns, and the element in %s would still believe it owns it.", compat.ArgS(exprText(st.Value)), compat.ArgS(x))
	d.Fix = pu.New(diag.Fix{Intro: "Range over the indexes and use a pointer to each element", Code: compat.Sprintfb("for i := range %s {\n\t%s := &%s[i]\n\t...\n}", compat.ArgS(x), compat.ArgS(exprText(st.Value)), compat.ArgS(x))})
	return d
}

// deferDiag explains a deferred call that uses an owner of an inner block.
func (c *checker) deferDiag(st *ast.DeferStmt, id *ast.Ident, v *types2.Var, b *ast.BlockStmt) *diag.Diagnostic {
	end := c.line(b.End())
	d := c.newDiag(st.Pos(), diag.DeferInnerOwner, "the deferred call uses %s, which is dropped at the end of its block (line %d)", pu.Box(v.Name()), end)
	d.Text = compat.Sprintfb("Deferred calls run when the function returns. %s is declared in the block that ends at line %d and is dropped there, before this call would run, so the call would use what %s had released.", compat.ArgS(v.Name()), compat.ArgI(int64(end)), compat.ArgS(v.Name()))
	d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Declare %s at the top level of the function, or do the work at the end of the block instead of deferring it.", compat.ArgS(v.Name()))})
	return d
}

// A dropKind says how Drop was reached other than on an owner variable.
type dropKind uint8

const (
	dropPointer dropKind = iota
	dropField
	dropIface
	dropMethodValue
)

// dropDiag explains a call of Drop through a view.
func (c *checker) dropDiag(se *ast.SelectorExpr, k dropKind) *diag.Diagnostic {
	x := exprText(se.X)
	xt := c.typeOf(se.X)
	var d *diag.Diagnostic
	switch k {
	case dropPointer:
		el := xt
		if p := pointerElem(xt); p != nil {
			el = p
		}
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot call %s.Drop(): %s is %s, which only points at %s that something else owns", pu.Box(x), pu.Box(x), pu.Box(article(c.typeName(xt))), pu.Box(article(c.typeName(el))))
		d.Text = compat.Sprintfb("Drop releases what an owner holds. The %s %s points to belongs to another variable or place, which will drop it when its life ends, so dropping it here would release it twice.", compat.ArgS(c.typeName(el)), compat.ArgS(x))
		d.Fix = pu.New(diag.Fix{Intro: "To end an owner's life early, call Drop on the variable that holds it; it cannot be used afterwards", Code: compat.Sprintfb("v.Drop() // v is the %s variable itself", compat.ArgS(c.typeName(el)))})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "To leave an empty value behind instead", Code: compat.Sprintfb("old := take(%s)", compat.ArgS(x))})})
	case dropField:
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot call %s.Drop(): %s is part of another value, which drops it when it is dropped", pu.Box(x), pu.Box(x))
		d.Text = compat.Sprintfb("Drop releases what an owner holds. %s belongs to the value it is part of, which will drop it again when its own life ends.", compat.ArgS(x))
		d.Fix = pu.New(diag.Fix{Intro: "To release it now and leave an empty value in its place", Code: compat.Sprintfb("old := take(&%s)\nold.Drop()", compat.ArgS(x))})
	case dropIface:
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot call %s.Drop() through %s, %s: an ordinary interface only lends the value it holds", pu.Box(x), pu.Box(x), pu.Box(article(c.typeName(xt))))
		d.Text = compat.Sprintfb("Converting an owner to an ordinary interface lends it: the owner still belongs to the variable that was converted, which will drop it. Dropping it through %s would release it twice.", compat.ArgS(x))
		d.Fix = pu.New(diag.Fix{Intro: "Call Drop on the owner itself, or declare an owning interface (type R owner interface {...}) to move the owner into it."})
	case dropMethodValue:
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot use %s.Drop as a value: Drop can only be called on the owner itself", pu.Box(x))
		d.Text = "A method value could be called at any time, by anyone, and each call would release what the owner holds."
		d.Fix = pu.New(diag.Fix{Intro: compat.Sprintfb("Call %s.Drop() where the owner's life should end.", compat.ArgS(x))})
	}
	return d
}

// methodValueDiag explains a method value of a consuming method. For a
// method promoted from an embedded owner, the receiver is that field.
func (c *checker) methodValueDiag(se *ast.SelectorExpr, recv types2.Type) *diag.Diagnostic {
	x, m := exprText(se.X), se.Sel.Name
	r, call := x, compat.Concat(x, ".", m, "()")
	if sel := c.selections[se]; sel != nil && len(sel.Index()) > 1 {
		r = embeddedPath(c.typeOf(se.X), x, sel.Index())
		call = compat.Concat("take(&", r, ").", m, "()")
	}
	T := c.typeName(recv)
	d := c.newDiag(se.Pos(), diag.ConsumingMethodValue, "cannot use %s.%s as a value: %s has a %s receiver, so it consumes %s", pu.Box(x), pu.Box(m), pu.Box(m), pu.Box(T), pu.Box(r))
	d.Text = compat.Sprintfb("%s A method with a value receiver of an owner type consumes its receiver: a call moves the receiver into the method. A method value would hold %s, and every call of it would consume %s again, so a consuming method has none.", compat.ArgS(c.ownerIntro(recv)), compat.ArgS(r), compat.ArgS(r))
	d.Fix = pu.New(diag.Fix{Intro: "Call the method where it is needed", Code: call})
	arg := r
	if r != x {
		arg = compat.Concat("take(&", r, ")")
	}
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or use the method expression, which takes the receiver as its first argument", Code: compat.Sprintfb("f := %s.%s\nf(%s)", compat.ArgS(T), compat.ArgS(m), compat.ArgS(arg))})})
	return d
}

// embeddedPath returns x followed by the names of the embedded fields the
// selection path index goes through to reach a promoted method.
func embeddedPath(T types2.Type, x string, index []int) string {
	for _, i := range index[:len(index)-1] {
		if e := pointerElem(T); e != nil {
			T = e
		}
		st, ok := T.Underlying().(*types2.Struct)
		if !ok {
			return x
		}
		fld := st.Field(i)
		x = compat.Concat(x, ".", fld.Name())
		T = fld.Type()
	}
	return x
}

// copyDiag explains copy or append(s, t...) of owner elements.
func (c *checker) copyDiag(e *ast.CallExpr, elem types2.Type, what string) *diag.Diagnostic {
	d := c.newDiag(e.Pos(), diag.CopyOwners, "cannot %s elements of type %s: each element is an owner", pu.Box(what), pu.Box(c.typeName(elem)))
	d.Text = compat.Sprintfb("%s duplicates elements, and duplicating an owner would leave two values that both release what it owns.", compat.ArgS(what))
	src := "src"
	if len(e.Args) == 2 {
		a := ast.Unparen(e.Args[1])
		if sl, ok := a.(*ast.SliceExpr); ok && sl.Low == nil && sl.High == nil {
			a = sl.X // s[:] is s, element for element
		}
		src = exprText(a)
	}
	d.Fix = pu.New(diag.Fix{Intro: "Move the elements one at a time", Code: compat.Sprintfb("for i := range %s {\n\tdst[i] = take(&%s[i])\n}", compat.ArgS(src), compat.ArgS(src))})
	if hasClone(elem) {
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "or copy each one explicitly", Code: compat.Sprintfb("for i := range %s {\n\tdst[i] = %s[i].Clone()\n}", compat.ArgS(src), compat.ArgS(src))})})
	}
	return d
}

// variadicDiag explains owners passed as variadic arguments.
func (c *checker) variadicDiag(a ast.Expr, callee string, elem types2.Type) *diag.Diagnostic {
	d := c.newDiag(a.Pos(), diag.VariadicOwners, "cannot pass %s to %s as a variadic argument: %s is an owner type", pu.Box(exprText(a)), pu.Box(callee), pu.Box(c.typeName(elem)))
	d.Text = compat.Sprintfb("Variadic arguments are passed in an array that %s sees as a slice. A slice is a view and never owns its elements, so %s would be released by nobody.", compat.ArgS(callee), compat.ArgS(exprText(a)))
	d.Fix = pu.New(diag.Fix{Intro: "Pass the owners one at a time to a parameter that takes them by value, or collect them in an owner such as a vec.Vec."})
	return d
}

// sliceLitDiag explains a slice literal of owners.
func (c *checker) sliceLitDiag(e *ast.CompositeLit, elem types2.Type) *diag.Diagnostic {
	d := c.newDiag(e.Pos(), diag.SliceLitOwners, "a slice literal cannot hold owners: %s is an owner type, and nothing would drop the elements", pu.Box(c.typeName(elem)))
	d.Text = "A slice literal keeps its elements in an array that the slice only looks at. Owners stored there would be released by nobody."
	d.Fix = pu.New(diag.Fix{Intro: "Use an array literal, which owns its elements", Code: compat.Sprintfb("[...]%s{...}", compat.ArgS(c.typeName(elem)))})
	return d
}

// assertDiag explains an owner asserted out of an ordinary interface.
func (c *checker) assertDiag(x ast.Expr, xt, T types2.Type, pos token.Pos) *diag.Diagnostic {
	xs := exprText(x)
	d := c.newDiag(pos, diag.OwnerFromLender, "cannot assert %s to %s: %s is %s, which only lends the value it holds", pu.Box(xs), pu.Box(c.typeName(T)), pu.Box(xs), pu.Box(article(c.typeName(xt))))
	d.Text = compat.Sprintfb("Converting an owner to an ordinary interface such as %s lends it: the interface looks at the owner, which still belongs to the variable that was converted. Taking %s out of %s would create a second owner.", compat.ArgS(c.typeName(xt)), compat.ArgS(article(c.typeName(T))), compat.ArgS(xs))
	d.Fix = pu.New(diag.Fix{Intro: "Call the value's methods through an interface type instead", Code: compat.Sprintfb("if s, ok := %s.(fmt.Stringer); ok {\n\tuse(s.String())\n}", compat.ArgS(xs))})
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "or move it into an owning interface in the first place", Code: "type Text owner interface{ String() string }"})})
	return d
}

// indexDiag explains x[k] on a container of owners.
func (c *checker) indexDiag(e *ast.IndexExpr) *diag.Diagnostic {
	x := exprText(e.X)
	T := c.typeOf(e)
	d := c.newDiag(e.Pos(), diag.IndexOwner, "cannot read %s: the elements of %s are owners, and %s would be a copy", pu.Box(exprText(e)), pu.Box(x), pu.Box(exprText(e)))
	d.Text = compat.Sprintfb("%s on %s calls its Index method, which returns a copy of the element. %s is an owner type, so the copy would be a second owner of what the element owns.", compat.ArgS(exprText(e)), compat.ArgS(c.typeName(c.typeOf(e.X))), compat.ArgS(c.typeName(T)))
	d.Fix = pu.New(diag.Fix{Intro: "To look at the element, use a method that returns a pointer to it", Code: compat.Sprintfb("p := %s.Ptr(%s)", compat.ArgS(x), compat.ArgS(exprText(e.Index)))})
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "To move it out, use a method that removes or replaces it, or take it through such a pointer", Code: compat.Sprintfb("elem := take(%s.Ptr(%s))", compat.ArgS(x), compat.ArgS(exprText(e.Index)))})})
	return d
}

// boxElem names the type an owned box refers to, or the box type itself
// for a type that is not a plain box.
func boxElem(c *checker, T types2.Type) string {
	if w, ok := types2.Unalias(T).Underlying().(*types2.Wrapper); ok && w.IsBox() {
		return c.typeName(w.Elem())
	}
	return c.typeName(T)
}
