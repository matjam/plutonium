// Translated by go2pu from internal/escape2/report/loc.go at the go-cutover tag; edited by hand since.

package report

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"pluto/path/filepath"
	"pluto/strings"
)

// A locKind says what memory a view looks at, in the terms a message uses.
// The kinds follow the frame objects of the analysis (eir.ObjKind), told
// apart further by the source construct that made them.
type locKind uint8

const (
	lkVar         locKind = iota // a local variable
	lkParam                      // a parameter
	lkResult                     // a named result
	lkTemp                       // a temporary copy of a value whose address is taken
	lkRecv                       // a temporary copy of a receiver
	lkStmtTemp                   // an owner temporary that lives until its statement ends
	lkLit                        // a composite literal whose address is taken
	lkSliceLit                   // the array of a slice literal
	lkVarargs                    // the array of packed variadic arguments
	lkBox                        // an interface's copy of a value
	lkObject                     // the object of an owner or counted reference (box, map, channel), viewed or lent
	lkClosure                    // a function literal's record
	lkMethodValue                // a method value's record
	lkCtor                       // the region of a call that returns closures (section 11.5)
	lkLoopBody                   // the body of a range loop over a function, as a closure
	lkArena                      // memory of a local arena
)

// A loc is what a view looks at: the frame memory a finding is about.
type loc struct {
	kind locKind
	fn   string      // the function whose frame holds it, as messages name it
	v    *types2.Var // the variable, for lkVar, lkParam, lkResult and a named lkArena
	pos  token.Pos   // where it is declared or made
	typ  types2.Type // its type (lkBox: the type of the value copied)
	node ast.Node    // the expression that makes it, if known

	captures []*types2.Var // lkClosure, lkLoopBody: the variables the literal uses
	object   bool          // lkStmtTemp: the temporary is an owner whose object is viewed
	method   string        // lkStmtTemp: the method called on the temporary
	ref      string        // lkObject: "map" or "channel" for a counted reference, "" for a box
	// lkBox of a value of a type parameter: the parameter, and a type
	// argument the program gives it whose values are copied ("" if none is
	// known).
	tparam *types2.TypeParam
	tparg  types2.Type
	loop   int // the loop of the function it is made in, or -1
}

func (l *loc) isVarLoc() bool {
	return l.kind == lkVar || l.kind == lkParam || l.kind == lkResult
}

func possessive(name string) string { return compat.Concat(name, "'s") }

func isArrayVar(v *types2.Var) bool {
	if v == nil {
		return false
	}
	_, ok := v.Type().Underlying().(*types2.Array)
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
	case lkResult:
		return "a result"
	}
	if isArrayVar(l.v) {
		return "a local array"
	}
	return "a local variable"
}

// name is what a message calls l in "which outlives x".
func (l *loc) name() string {
	switch {
	case l.isVarLoc(), (l.kind == lkArena || l.kind == lkObject) && l.v != nil:
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
	return strings.ReplaceAll(types2.ExprString(e), "…", "...")
}

// basePos writes a position as file:line, without the directory.
func (r *reporter) basePos(p token.Pos) string {
	pp := r.fset.Position(p)
	return compat.Sprintfb("%s:%d", compat.ArgS(filepath.Base(pp.Filename)), compat.ArgI(int64(pp.Line)))
}

func (r *reporter) line(p token.Pos) int { return r.fset.Position(p).Line }

// noun describes l with its name: "x, a local variable of load".
func (r *reporter) noun(l *loc, q types2.Qualifier) string {
	frame := compat.Concat(possessive(l.fn), " frame")
	switch l.kind {
	case lkVar, lkParam, lkResult:
		return compat.Concat(l.v.Name(), ", ", l.kindWord(), " of ", l.fn)
	case lkTemp:
		return compat.Concat("a temporary copy in ", frame)
	case lkRecv:
		return compat.Concat("a temporary copy of a receiver in ", frame)
	case lkStmtTemp:
		if e, ok := l.node.(ast.Expr); ok {
			return compat.Concat("the temporary ", typeText(l.typ, q), " that ", exprText(e), " returns")
		}
		return compat.Concat("a temporary ", typeText(l.typ, q), " in ", frame)
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
		if e, ok := l.node.(ast.Expr); ok {
			return compat.Concat("the closure ", exprText(e), " returns, built in ", frame)
		}
		return compat.Concat("a closure built in ", frame)
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

// intro explains where l lives, in a sentence or two.
func (r *reporter) intro(l *loc, q types2.Qualifier) string {
	at := r.basePos(l.pos)
	switch l.kind {
	case lkVar, lkParam, lkResult:
		s := compat.Sprintfb("%s is %s of %s (declared at %s).", compat.ArgS(l.v.Name()), compat.ArgS(l.kindWord()), compat.ArgS(l.fn), compat.ArgS(at))
		if l.kind != lkVar {
			s = compat.Concat(s, compat.Concat(" It lives in ", possessive(l.fn), " frame, like a local variable."))
		}
		return s
	case lkTemp:
		return compat.Sprintfb("Taking the address of a value that is not a variable (at %s) copies the value to a temporary in %s frame.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkRecv:
		return compat.Sprintfb("Calling a pointer method on a value that is not a variable (at %s) copies the value to a temporary in %s frame, and the method gets a pointer to the copy.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkStmtTemp:
		if l.object {
			return compat.Sprintfb("The %s made at %s is a temporary: nothing keeps it, so its object is dropped at the end of the statement. A view of the object may be passed to calls in that statement but may not outlive it.", compat.ArgS(typeText(l.typ, q)), compat.ArgS(at))
		}
		what := "a call"
		if e, ok := l.node.(ast.Expr); ok {
			what = exprText(e)
		}
		return compat.Sprintfb("The %s that %s returns (at %s) is a temporary: nothing keeps it, so it is the receiver of the method only until the end of the statement. A view of it may be passed to calls in that statement but may not outlive it.", compat.ArgS(typeText(l.typ, q)), compat.ArgS(what), compat.ArgS(at))
	case lkLit:
		return compat.Sprintfb("A composite literal whose address is taken (at %s) is made in %s frame, as a local variable would be.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkSliceLit:
		return compat.Sprintfb("A slice literal (at %s) keeps its elements in an array in %s frame, and the slice looks at that array.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkVarargs:
		return compat.Sprintfb("A call with variadic arguments (at %s) passes them in an array in %s frame, and the function gets a slice of that array.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkBox:
		if l.tparam != nil {
			return compat.Concat(compat.Sprintfb("Converting a value of type parameter %s to an interface puts the value in the interface itself when the type argument fits in one word, such as a pointer, and otherwise puts a copy of the value in the frame of the function that converts it (here %s, at %s), and the interface points at that copy.", compat.ArgS(l.tparam.Obj().Name()), compat.ArgS(l.fn), compat.ArgS(at)), r.tpargUse(l, q))
		}
		return compat.Sprintfb("Converting a value larger than one word to an interface puts a copy of the value in the frame of the function that converts it (here %s, at %s), and the interface points at that copy.", compat.ArgS(l.fn), compat.ArgS(at))
	case lkObject:
		ref := l.ref
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
	case lkClosure:
		decl := ""
		if len(l.captures) > 0 {
			decl = compat.Sprintfb(" (%s is declared at %s)", compat.ArgS(l.captures[0].Name()), compat.ArgS(r.basePos(l.captures[0].Pos())))
		}
		return compat.Sprintfb("A function literal that uses variables of the function around it refers to those variables where they live, in %s frame%s.", compat.ArgS(possessive(l.fn)), compat.ArgS(decl))
	case lkMethodValue:
		return compat.Sprintfb("A method value (at %s) is a record in %s frame that holds its receiver, and calling it calls the method on that receiver.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkLoopBody:
		return compat.Sprintfb("A range loop over a function (at %s) calls the function with a yield function whose body is the loop body, a closure built in %s frame (section 12.11); yield may be called only while the loop runs.", compat.ArgS(at), compat.ArgS(possessive(l.fn)))
	case lkCtor:
		what := "the call"
		if e, ok := l.node.(ast.Expr); ok {
			what = exprText(e)
		}
		return compat.Sprintfb("%s returns a closure, which it builds, with the variables it uses, in a region of its caller's frame, %s (at %s); they live as long as that frame (section 11.5).", compat.ArgS(what), compat.ArgS(possessive(l.fn)), compat.ArgS(at))
	case lkArena:
		name := "a local arena"
		if l.v != nil {
			name = compat.Concat(l.v.Name(), " (a local arena of ", l.fn, ")")
		}
		what := "alloc.Use"
		if e, ok := l.node.(ast.Expr); ok {
			what = exprText(e)
		}
		if l.v != nil {
			// The arena variable: the alloc.Use that makes it the default,
			// else the call it is given to as an allocator.
			use, given := r.arenaUse(l.v)
			if use == nil {
				to := ""
				if given != nil {
					to = compat.Sprintfb(" to %s (at %s)", compat.ArgS(exprText(given)), compat.ArgS(r.basePos(given.Pos())))
				}
				return compat.Sprintfb("%s (a local arena of %s, declared at %s) is given as an allocator%s, so what is allocated from it lives in the arena, and is gone when the arena is.", compat.ArgS(l.v.Name()), compat.ArgS(l.fn), compat.ArgS(at), compat.ArgS(to))
			}
			what, at = exprText(use), r.basePos(use.Pos())
		}
		return compat.Sprintfb("%s (at %s) makes %s the default allocator, so what is allocated through the default allocator from there on (by new, a string operation, or a call that allocates and leaves out its allocator) lives in the arena, and is gone when the arena is.", compat.ArgS(what), compat.ArgS(at), compat.ArgS(name))
	}
	return ""
}

// arenaUse returns, in the function that declares the local arena v, the
// first call alloc.Use that is given v, or else nil and the first other
// call that is given it.
func (r *reporter) arenaUse(v *types2.Var) (use, given *ast.CallExpr) {
	fn := r.funcScope(v.Pos())
	info := r.info(v.Pos())
	if fn == nil || info == nil {
		return nil, nil
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || use != nil {
			return use == nil
		}
		for _, a := range ce.Args {
			if !mentions(a, v, info) {
				continue
			}
			if f := r.calledFunc(ce); f != nil && f.Name() == "Use" && f.Pkg() != nil && f.Pkg().Path() == "pluto/alloc" {
				use = ce
				return false
			}
			if given == nil {
				given = ce
			}
		}
		return true
	})
	return use, given
}
