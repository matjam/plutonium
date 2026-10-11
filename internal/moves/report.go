package moves

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
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
	return fmt.Sprintf("%s is an owner type: its values own what they hold and release it when they are dropped.", c.typeName(T))
}

// howMoved says how a value left a variable: "moved to n", "moved into
// consume (its parameter takes a Buf by value)".
func (c *checker) howMoved(mv *move) string {
	switch mv.kind {
	case mvAssign:
		return "moved to " + mv.to
	case mvArg:
		return fmt.Sprintf("moved into %s (its parameter takes %s by value)", mv.to, article(c.typeName(mv.typ)))
	case mvRecv:
		name := mv.to
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		return fmt.Sprintf("moved by the call %s() (%s has a value receiver)", mv.to, name)
	case mvReturn:
		return "returned"
	case mvSend:
		return "moved by the send on " + mv.to
	case mvGo:
		return "moved to the task started"
	case mvGoRecv:
		return "moved to the task started by task " + mv.to + "()"
	case mvTask:
		return "taken by the task started by " + mv.to
	case mvStore:
		return "moved into " + mv.to
	case mvDrop:
		return "dropped by " + mv.to + "()"
	case mvConv:
		return "converted to " + mv.to
	case mvMethodValue:
		return "moved into the method value " + mv.to
	case mvAssert:
		return "moved out by the type assertion " + mv.to
	case mvNewValue:
		return "given a new value"
	case mvTake:
		return "emptied by " + mv.to
	case mvScopeEnd:
		return "dropped at the end of its block"
	}
	return "moved"
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
		d = c.newDiag(e.Pos(), diag.StaleView, "%s holds a view of %s (%s, line %d), which may have been %s at line %d, so %s cannot be used here", name, owner, w.text, made, how, at, name)
	} else {
		d = c.newDiag(e.Pos(), diag.StaleView, "%s holds a view of %s (%s, line %d), which was %s at line %d, so %s can no longer be used here", name, owner, w.text, made, how, at, name)
	}
	holder := name
	if name != v.Name() {
		holder = name + ", part of " + v.Name() + ","
	}
	lost := fmt.Sprintf("At line %d %s was %s", at, owner, how)
	if w.maybe {
		lost = fmt.Sprintf("At line %d %s was %s on some paths to this point", at, owner, how)
	}
	d.Text = fmt.Sprintf("%s looks at memory that %s owns, without owning it: the view was made by %s at line %d. A view of an owner held in a local variable may be used only while the variable keeps the value the view was made from. %s, so the view may look at memory that was released, or at a value that now belongs to something else.", holder, owner, w.text, made, lost)
	switch w.stale.kind {
	case mvNewValue:
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Use %s before line %d, or give %s its new value only after the last use of %s", name, at, owner, name)}
		d.More = []*diag.Fix{{Intro: fmt.Sprintf("Or make the view again after line %d", at), Code: fmt.Sprintf("%s = %s", name, w.text)}}
	case mvScopeEnd:
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Declare %s in a block that encloses every use of %s, or use %s only inside the block of %s", owner, name, name, owner)}
	default:
		act := "move " + owner
		switch w.stale.kind {
		case mvDrop:
			act = "drop " + owner
		case mvTake:
			act = "empty " + owner
		}
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Use %s before line %d, or %s only after the last use of %s", name, at, act, name)}
		if hasClone(w.of.Type()) && w.stale.kind != mvDrop && w.stale.kind != mvTake {
			d.More = []*diag.Fix{{Intro: fmt.Sprintf("Or move a copy at line %d, so that %s keeps its value", at, owner), Code: owner + ".Clone()"}}
		}
	}
	return d
}

// lostVerb says how an owner loses its value, for a sentence about it: "is
// moved into consume (...)".
func (c *checker) lostVerb(mv move) string {
	return "is " + c.howMoved(&mv)
}

// pinnedDiag explains moving, dropping or replacing the value of v while a
// view of it is kept where the move checker cannot follow it.
func (c *checker) pinnedDiag(v *types2.Var, p *pinMark, mv move) *diag.Diagnostic {
	owner := v.Name()
	d := c.newDiag(mv.pos, diag.LostView, "%s %s here, but a view of it (%s, line %d) was %s at line %d, where the compiler cannot check its uses", owner, c.lostVerb(mv), p.text, c.line(p.pos), p.how, c.line(p.where))
	d.Text = fmt.Sprintf("A view of an owner held in a local variable may be used only while the variable keeps the value the view was made from. The compiler follows views through local variables and their fields, and into the arguments of the calls that store them there, but the view of %s made by %s at line %d went into memory it does not follow, so it cannot check that the view is not used after line %d.", owner, p.text, c.line(p.pos), c.line(mv.pos))
	d.Fix = &diag.Fix{Intro: fmt.Sprintf("Keep the view in a local variable or a field of one, where the compiler follows it, or do not %s while the view is kept", strings.TrimPrefix(c.lostVerbBase(mv), "is "))}
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
			return "new(" + c.typeName(u.Elem()) + ")"
		}
	case *types2.TaggedUnion:
		if u.NumVariants() > 0 {
			v := c.typeName(T) + "." + u.Variant(0).Name()
			if u.Variant(0).HasPayload() {
				v += "(...)"
			}
			return v
		}
	case *types2.Basic:
		return c.typeName(T) + "(0)"
	}
	return c.typeName(T) + "{}"
}

func article(s string) string {
	if s != "" && strings.ContainsRune("aeiouAEIOU", rune(s[0])) {
		return "an " + s
	}
	return "a " + s
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
		d := c.newDiag(id.Pos(), diag.UseMaybeMoved, "%s was %s at line %d in the previous iteration of this loop", name, c.howMoved(mv), at)
		d.Text = fmt.Sprintf("%s %s is declared outside the loop, and the loop moves its value away at line %d without giving it a new one, so when the next iteration starts %s holds nothing.", c.ownerIntro(T), name, at, name)
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Declare %s inside the loop, so that each iteration has its own", name), Code: fmt.Sprintf("for ... {\n\tvar %s %s\n\t...\n}", name, c.typeName(T))}
		more := &diag.Fix{Intro: fmt.Sprintf("Or give %s a new value after line %d", name, at), Code: fmt.Sprintf("%s = %s", name, c.newValue(T))}
		d.More = []*diag.Fix{more}
		if hasClone(T) {
			d.More = append(d.More, &diag.Fix{Intro: fmt.Sprintf("Or move a copy at line %d", at), Code: name + ".Clone()"})
		}
		return d
	}
	if s.st == maybe {
		d := c.newDiag(id.Pos(), diag.UseMaybeMoved, "%s may have been moved at line %d, so it cannot be used here", name, at)
		d.Text = fmt.Sprintf("%s %s is %s at line %d on some paths to this point and not on others. On those paths %s holds nothing afterwards, and the compiler cannot let a value be used where it may already belong to someone else.", c.ownerIntro(T), name, c.howMoved(mv), at, name)
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Use %s before line %d, or give it a new value on the path that moves it", name, at), Code: fmt.Sprintf("%s = %s", name, c.newValue(T))}
		return d
	}
	d := c.newDiag(id.Pos(), diag.UseAfterMove, "%s was %s at line %d, so it can no longer be used here", name, c.howMoved(mv), at)
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
	text := fmt.Sprintf("%s %s instead of copying it, so that what it owns has exactly one owner.", c.ownerIntro(T), what)
	if mv.kind == mvDrop {
		text = fmt.Sprintf("%s %s: what %s owned has been released.", c.ownerIntro(T), what, name)
	} else if holder != "it" {
		text += fmt.Sprintf(" Since line %d, %s owns it and %s holds nothing.", at, holder, name)
	} else {
		text += fmt.Sprintf(" Since line %d, %s holds nothing.", at, name)
	}
	d.Text = text
	var fixes []*diag.Fix
	switch mv.kind {
	case mvAssign:
		if hasClone(T) {
			fixes = append(fixes, &diag.Fix{Intro: fmt.Sprintf("If you need two independent values, copy it explicitly at line %d", at), Code: fmt.Sprintf("%s := %s.Clone()", mv.to, name)})
		}
		fixes = append(fixes, &diag.Fix{Intro: fmt.Sprintf("If %s only needs to look at the value, give it a pointer instead", mv.to), Code: fmt.Sprintf("%s := &%s", mv.to, name)})
	case mvArg:
		fixes = append(fixes, &diag.Fix{Intro: fmt.Sprintf("If %s only needs to look at the value, change its parameter to *%s and pass a pointer", mv.to, c.typeName(T)), Code: fmt.Sprintf("%s(&%s)", mv.to, name)})
		if hasClone(T) {
			fixes = append(fixes, &diag.Fix{Intro: "If both need a value, pass a copy", Code: fmt.Sprintf("%s(%s.Clone())", mv.to, name)})
		}
	case mvRecv:
		method := mv.to
		if i := strings.LastIndexByte(method, '.'); i >= 0 {
			method = method[i+1:]
		}
		fixes = append(fixes, &diag.Fix{Intro: fmt.Sprintf("Declare %s with a pointer receiver so that calling it does not consume %s", method, name), Code: fmt.Sprintf("func (x *%s) %s(...)", c.typeName(T), method)})
	case mvGoRecv:
		fixes = append(fixes, &diag.Fix{Intro: fmt.Sprintf("To keep using %s while the task runs, make it a synced box: the task then takes a reference of its own", name), Code: fmt.Sprintf("%s := alloc.Synced[%s]()", name, boxElem(c, T))})
	default:
		if hasClone(T) {
			fixes = append(fixes, &diag.Fix{Intro: fmt.Sprintf("If both need a value, move a copy at line %d", at), Code: name + ".Clone()"})
		}
	}
	fixes = append(fixes, &diag.Fix{Intro: fmt.Sprintf("If this is the last use of %s, move it before line %d, where %s still holds its value.", name, at, name)})
	d.Fix, d.More = fixes[0], fixes[1:]
	return d
}

// capturedDiag explains a move of a captured variable inside a literal.
func (c *checker) capturedDiag(id *ast.Ident, v *types2.Var) *diag.Diagnostic {
	name := v.Name()
	d := c.newDiag(id.Pos(), diag.MoveCaptured, "cannot move %s inside a function literal: the literal may run more than once, or never", name)
	d.Text = fmt.Sprintf("%s is a variable of the function around the literal (declared at %s), which the literal uses where it lives. Moving it here would leave %s empty after the first call, and the function could not tell whether %s still needs to be dropped.", name, c.basePos(v.Pos()), name, name)
	d.Fix = &diag.Fix{Intro: "Pass the value to the literal as an argument", Code: fmt.Sprintf("do := func(%s %s) { ... }\ndo(%s)", name, c.typeName(v.Type()), name)}
	return d
}

func (c *checker) basePos(p token.Pos) string {
	pp := c.fset.Position(p)
	f := pp.Filename
	if i := strings.LastIndexByte(f, '/'); i >= 0 {
		f = f[i+1:]
	}
	return fmt.Sprintf("%s:%d", f, pp.Line)
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
		first = fmt.Sprintf("cannot move %s out of %s: %s is a field, not a variable", x, root, x)
	case placeEmbedded:
		se := ast.Unparen(e).(*ast.SelectorExpr)
		root = exprText(se.X)
		first = fmt.Sprintf("cannot call %s: its value receiver would move an embedded field out of %s", x, root)
	case placeElem:
		if ix, ok := ast.Unparen(e).(*ast.IndexExpr); ok {
			root = exprText(ix.X)
		}
		first = fmt.Sprintf("cannot move %s out of %s: %s is an element, not a variable", x, root, x)
	case placePointer:
		if st, ok := ast.Unparen(e).(*ast.StarExpr); ok {
			root = exprText(st.X)
		}
		first = fmt.Sprintf("cannot move %s out of the value %s points to", x, root)
	case placeGlobal:
		first = fmt.Sprintf("cannot move package variable %s: only a local variable can be moved out of", x)
	}
	d := c.newDiag(e.Pos(), diag.MoveOutOfPlace, "%s", first)
	tn := "the value"
	if T != nil {
		tn = c.typeName(T)
	}
	switch k {
	case placeGlobal:
		d.Text = fmt.Sprintf("%s is an owner type, so using %s here would move it. But a package variable lives for the whole program and would still believe it holds the value.", tn, x)
	case placePointer:
		d.Text = fmt.Sprintf("%s is an owner type, so using %s here would move it. But the value belongs to whatever %s points into, which would free it again when it is dropped.", tn, x, root)
	default:
		d.Text = fmt.Sprintf("%s is an owner type, so taking %s would move it. But %s would still believe it owns %s and would free it again when %s is dropped.", tn, x, root, x, root)
	}
	if k == placeEmbedded {
		d.Fix = &diag.Fix{Intro: "Declare the method with a pointer receiver, so that calling it borrows the field instead of moving it."}
		return d
	}
	d.Fix = &diag.Fix{Intro: "To move it out and leave an empty value in its place", Code: fmt.Sprintf("v := take(&%s)", x)}
	if k == placePointer {
		d.Fix.Code = fmt.Sprintf("v := take(%s)", root)
	}
	d.More = []*diag.Fix{{Intro: "To look at it without moving it", Code: fmt.Sprintf("v := &%s", x)}}
	if k == placePointer {
		d.More[0].Code = fmt.Sprintf("v := %s", root)
	}
	if T != nil && hasClone(T) {
		d.More = append(d.More, &diag.Fix{Intro: "To copy it", Code: fmt.Sprintf("v := %s.Clone()", x)})
	}
	return d
}

// rangeDiag explains a range loop that would copy owner elements.
func (c *checker) rangeDiag(st *ast.RangeStmt, elem types2.Type) *diag.Diagnostic {
	x := exprText(st.X)
	d := c.newDiag(st.Value.Pos(), diag.RangeCopiesOwner, "cannot copy the elements of %s in a range loop: %s is an owner type", x, c.typeName(elem))
	d.Text = fmt.Sprintf("A range loop copies each element into %s. Copying an owner would leave two values that both release what it owns, and the element in %s would still believe it owns it.", exprText(st.Value), x)
	d.Fix = &diag.Fix{Intro: "Range over the indexes and use a pointer to each element", Code: fmt.Sprintf("for i := range %s {\n\t%s := &%s[i]\n\t...\n}", x, exprText(st.Value), x)}
	return d
}

// deferDiag explains a deferred call that uses an owner of an inner block.
func (c *checker) deferDiag(st *ast.DeferStmt, id *ast.Ident, v *types2.Var, b *ast.BlockStmt) *diag.Diagnostic {
	end := c.line(b.End())
	d := c.newDiag(st.Pos(), diag.DeferInnerOwner, "the deferred call uses %s, which is dropped at the end of its block (line %d)", v.Name(), end)
	d.Text = fmt.Sprintf("Deferred calls run when the function returns. %s is declared in the block that ends at line %d and is dropped there, before this call would run, so the call would use what %s had released.", v.Name(), end, v.Name())
	d.Fix = &diag.Fix{Intro: fmt.Sprintf("Declare %s at the top level of the function, or do the work at the end of the block instead of deferring it.", v.Name())}
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
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot call %s.Drop(): %s is %s, which only points at %s that something else owns", x, x, article(c.typeName(xt)), article(c.typeName(el)))
		d.Text = fmt.Sprintf("Drop releases what an owner holds. The %s %s points to belongs to another variable or place, which will drop it when its life ends, so dropping it here would release it twice.", c.typeName(el), x)
		d.Fix = &diag.Fix{Intro: "To end an owner's life early, call Drop on the variable that holds it; it cannot be used afterwards", Code: fmt.Sprintf("v.Drop() // v is the %s variable itself", c.typeName(el))}
		d.More = []*diag.Fix{{Intro: "To leave an empty value behind instead", Code: fmt.Sprintf("old := take(%s)", x)}}
	case dropField:
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot call %s.Drop(): %s is part of another value, which drops it when it is dropped", x, x)
		d.Text = fmt.Sprintf("Drop releases what an owner holds. %s belongs to the value it is part of, which will drop it again when its own life ends.", x)
		d.Fix = &diag.Fix{Intro: "To release it now and leave an empty value in its place", Code: fmt.Sprintf("old := take(&%s)\nold.Drop()", x)}
	case dropIface:
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot call %s.Drop() through %s, %s: an ordinary interface only lends the value it holds", x, x, article(c.typeName(xt)))
		d.Text = fmt.Sprintf("Converting an owner to an ordinary interface lends it: the owner still belongs to the variable that was converted, which will drop it. Dropping it through %s would release it twice.", x)
		d.Fix = &diag.Fix{Intro: "Call Drop on the owner itself, or declare an owning interface (type R owner interface {...}) to move the owner into it."}
	case dropMethodValue:
		d = c.newDiag(se.Pos(), diag.DropThroughView, "cannot use %s.Drop as a value: Drop can only be called on the owner itself", x)
		d.Text = "A method value could be called at any time, by anyone, and each call would release what the owner holds."
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Call %s.Drop() where the owner's life should end.", x)}
	}
	return d
}

// copyDiag explains copy or append(s, t...) of owner elements.
func (c *checker) copyDiag(e *ast.CallExpr, elem types2.Type, what string) *diag.Diagnostic {
	d := c.newDiag(e.Pos(), diag.CopyOwners, "cannot %s elements of type %s: each element is an owner", what, c.typeName(elem))
	d.Text = fmt.Sprintf("%s duplicates elements, and duplicating an owner would leave two values that both release what it owns.", what)
	src := "src"
	if len(e.Args) == 2 {
		a := ast.Unparen(e.Args[1])
		if sl, ok := a.(*ast.SliceExpr); ok && sl.Low == nil && sl.High == nil {
			a = sl.X // s[:] is s, element for element
		}
		src = exprText(a)
	}
	d.Fix = &diag.Fix{Intro: "Move the elements one at a time", Code: fmt.Sprintf("for i := range %s {\n\tdst[i] = take(&%s[i])\n}", src, src)}
	if hasClone(elem) {
		d.More = []*diag.Fix{{Intro: "or copy each one explicitly", Code: fmt.Sprintf("for i := range %s {\n\tdst[i] = %s[i].Clone()\n}", src, src)}}
	}
	return d
}

// variadicDiag explains owners passed as variadic arguments.
func (c *checker) variadicDiag(a ast.Expr, callee string, elem types2.Type) *diag.Diagnostic {
	d := c.newDiag(a.Pos(), diag.VariadicOwners, "cannot pass %s to %s as a variadic argument: %s is an owner type", exprText(a), callee, c.typeName(elem))
	d.Text = fmt.Sprintf("Variadic arguments are passed in an array that %s sees as a slice. A slice is a view and never owns its elements, so %s would be released by nobody.", callee, exprText(a))
	d.Fix = &diag.Fix{Intro: "Pass the owners one at a time to a parameter that takes them by value, or collect them in an owner such as a vec.Vec."}
	return d
}

// sliceLitDiag explains a slice literal of owners.
func (c *checker) sliceLitDiag(e *ast.CompositeLit, elem types2.Type) *diag.Diagnostic {
	d := c.newDiag(e.Pos(), diag.SliceLitOwners, "a slice literal cannot hold owners: %s is an owner type, and nothing would drop the elements", c.typeName(elem))
	d.Text = "A slice literal keeps its elements in an array that the slice only looks at. Owners stored there would be released by nobody."
	d.Fix = &diag.Fix{Intro: "Use an array literal, which owns its elements", Code: fmt.Sprintf("[...]%s{...}", c.typeName(elem))}
	return d
}

// assertDiag explains an owner asserted out of an ordinary interface.
func (c *checker) assertDiag(x ast.Expr, xt, T types2.Type, pos token.Pos) *diag.Diagnostic {
	xs := exprText(x)
	d := c.newDiag(pos, diag.OwnerFromLender, "cannot assert %s to %s: %s is %s, which only lends the value it holds", xs, c.typeName(T), xs, article(c.typeName(xt)))
	d.Text = fmt.Sprintf("Converting an owner to an ordinary interface such as %s lends it: the interface looks at the owner, which still belongs to the variable that was converted. Taking %s out of %s would create a second owner.", c.typeName(xt), article(c.typeName(T)), xs)
	d.Fix = &diag.Fix{Intro: "Call the value's methods through an interface type instead", Code: fmt.Sprintf("if s, ok := %s.(fmt.Stringer); ok {\n\tuse(s.String())\n}", xs)}
	d.More = []*diag.Fix{{Intro: "or move it into an owning interface in the first place", Code: "type Text owner interface{ String() string }"}}
	return d
}

// indexDiag explains x[k] on a container of owners.
func (c *checker) indexDiag(e *ast.IndexExpr) *diag.Diagnostic {
	x := exprText(e.X)
	T := c.typeOf(e)
	d := c.newDiag(e.Pos(), diag.IndexOwner, "cannot read %s: the elements of %s are owners, and %s would be a copy", exprText(e), x, exprText(e))
	d.Text = fmt.Sprintf("%s on %s calls its Index method, which returns a copy of the element. %s is an owner type, so the copy would be a second owner of what the element owns.", exprText(e), c.typeName(c.typeOf(e.X)), c.typeName(T))
	d.Fix = &diag.Fix{Intro: "To look at the element, use a method that returns a pointer to it", Code: fmt.Sprintf("p := %s.Ptr(%s)", x, exprText(e.Index))}
	d.More = []*diag.Fix{{Intro: "To move it out, use a method that removes or replaces it, or take it through such a pointer", Code: fmt.Sprintf("elem := take(%s.Ptr(%s))", x, exprText(e.Index))}}
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
