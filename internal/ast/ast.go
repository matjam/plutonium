// Package ast declares the types used to represent syntax trees for Plutonium
// source files. Plutonium is a kernel-safe subset of Go.
package ast

import (
	"strings"

	"github.com/matjam/plutonium/internal/token"
)

// Node is the base interface for all AST nodes.
type Node interface {
	Pos() token.Pos
	End() token.Pos
}

// Expr is the interface for all expression nodes.
type Expr interface {
	Node
	exprNode()
}

// Stmt is the interface for all statement nodes.
type Stmt interface {
	Node
	stmtNode()
}

// Decl is the interface for all declaration nodes.
type Decl interface {
	Node
	declNode()
}

// ----------------------------------------------------------------------------
// File

// File represents a parsed Plutonium source file.
type File struct {
	Package    token.Pos
	Name       *Ident
	Decls      []Decl
	Directives []Directive
	Imports    []*ImportSpec
	Scope      *Scope
}

func (f *File) Pos() token.Pos { return f.Package }
func (f *File) End() token.Pos {
	if n := len(f.Decls); n > 0 {
		return f.Decls[n-1].End()
	}
	return f.Name.End()
}

// Directive represents a compiler directive (e.g. //go:noinline).
type Directive struct {
	Pos  token.Pos
	Text string
}

// ----------------------------------------------------------------------------
// Comments

// Comment represents a single //-style or /* */  comment.
type Comment struct {
	Slash token.Pos
	Text  string
}

func (c *Comment) Pos() token.Pos { return c.Slash }
func (c *Comment) End() token.Pos { return token.Pos(int(c.Slash) + len(c.Text)) }

// CommentGroup represents a sequence of comments with no other tokens and no
// empty lines between.
type CommentGroup struct {
	List []*Comment
}

func (g *CommentGroup) Pos() token.Pos { return g.List[0].Pos() }
func (g *CommentGroup) End() token.Pos { return g.List[len(g.List)-1].End() }

// Text returns the text of the comment group, with comment markers removed.
func (g *CommentGroup) Text() string {
	var b strings.Builder
	for _, c := range g.List {
		b.WriteString(c.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// ----------------------------------------------------------------------------
// Object and Scope

// ObjKind describes what an Object represents.
type ObjKind int

const (
	Bad ObjKind = iota // for error handling
	Pkg                // package
	Con                // constant
	Typ                // type
	Var                // variable
	Fun                // function or method
	Lbl                // label
)

var objKindStrings = [...]string{
	Bad: "bad",
	Pkg: "package",
	Con: "const",
	Typ: "type",
	Var: "var",
	Fun: "func",
	Lbl: "label",
}

func (k ObjKind) String() string {
	if int(k) < len(objKindStrings) {
		return objKindStrings[k]
	}
	return "ObjKind(" + strings.TrimSpace(strings.Repeat(" ", int(k))) + ")"
}

// Object represents a named language entity such as a package, constant,
// type, variable, function (excl. methods), or label.
type Object struct {
	Kind ObjKind
	Name string      // declared name
	Decl interface{} // corresponding Field, XxxSpec, FuncDecl, or nil
	Data interface{} // object-specific data; or nil
	Type interface{} // placeholder for type information; may be nil
}

// Scope maintains the set of named language entities declared in the scope
// and a link to the immediately surrounding (outer) scope.
type Scope struct {
	Outer   *Scope
	Objects map[string]*Object
}

// NewScope creates a new scope nested in the outer scope.
func NewScope(outer *Scope) *Scope {
	return &Scope{Outer: outer, Objects: make(map[string]*Object)}
}

// Lookup returns the object with the given name if it is found in scope s,
// otherwise it returns nil. Outer scopes are ignored.
func (s *Scope) Lookup(name string) *Object {
	return s.Objects[name]
}

// Insert attempts to insert a named object obj into scope s.
// If the scope already contains an object alt with the same name,
// Insert leaves the scope unchanged and returns alt. Otherwise it
// inserts obj and returns nil.
func (s *Scope) Insert(obj *Object) (alt *Object) {
	if alt = s.Objects[obj.Name]; alt == nil {
		s.Objects[obj.Name] = obj
	}
	return
}
