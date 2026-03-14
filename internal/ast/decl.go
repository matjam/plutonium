package ast

import "github.com/matjam/plutonium/internal/token"

// ----------------------------------------------------------------------------
// Declarations

// BadDecl is a placeholder for declarations containing syntax errors for which
// no correct declaration nodes can be created.
type BadDecl struct {
	From, To token.Pos // position range of bad declaration
}

func (d *BadDecl) Pos() token.Pos { return d.From }
func (d *BadDecl) End() token.Pos { return d.To }
func (d *BadDecl) declNode()      {}

// GenDecl represents an import, constant, type or variable declaration.
// A valid Lparen position (Lparen.IsValid()) indicates a parenthesized declaration.
type GenDecl struct {
	TokPos token.Pos   // position of Tok
	Tok    token.Token // IMPORT, CONST, TYPE, or VAR
	Lparen token.Pos   // position of '(', if any
	Specs  []Spec
	Rparen token.Pos // position of ')', if any
}

func (d *GenDecl) Pos() token.Pos { return d.TokPos }
func (d *GenDecl) End() token.Pos {
	if d.Rparen.IsValid() {
		return d.Rparen + 1
	}
	return d.Specs[0].End()
}
func (d *GenDecl) declNode() {}

// FuncDecl represents a function declaration.
type FuncDecl struct {
	Recv *FieldList // receiver (methods); or nil (functions)
	Name *Ident     // function/method name
	Type *FuncType  // function signature: type and value parameters, results, and position of "func" keyword
	Body *BlockStmt // function body; or nil for external (non-Go) function
}

func (d *FuncDecl) Pos() token.Pos { return d.Type.Pos() }
func (d *FuncDecl) End() token.Pos {
	if d.Body != nil {
		return d.Body.End()
	}
	return d.Type.End()
}
func (d *FuncDecl) declNode() {}

// ----------------------------------------------------------------------------
// Specs

// Spec is the interface for all spec nodes.
type Spec interface {
	Node
	specNode()
}

// ImportSpec represents a single package import.
type ImportSpec struct {
	Name    *Ident        // local package name (including "."); or nil
	Path    *BasicLit     // import path
	Comment *CommentGroup // line comments; or nil
	EndPos  token.Pos     // end of spec (overrides Path.Pos if nonzero)
}

func (s *ImportSpec) Pos() token.Pos {
	if s.Name != nil {
		return s.Name.Pos()
	}
	return s.Path.Pos()
}
func (s *ImportSpec) End() token.Pos {
	if s.EndPos.IsValid() {
		return s.EndPos
	}
	return s.Path.End()
}
func (s *ImportSpec) specNode() {}

// ValueSpec represents a constant or variable declaration (ConstSpec or VarSpec production).
type ValueSpec struct {
	Names   []*Ident      // value names (len(Names) > 0)
	Type    Expr          // value type; or nil
	Values  []Expr        // initial values; or nil
	Comment *CommentGroup // line comments; or nil
}

func (s *ValueSpec) Pos() token.Pos { return s.Names[0].Pos() }
func (s *ValueSpec) End() token.Pos {
	if n := len(s.Values); n > 0 {
		return s.Values[n-1].End()
	}
	if s.Type != nil {
		return s.Type.End()
	}
	return s.Names[len(s.Names)-1].End()
}
func (s *ValueSpec) specNode() {}

// TypeSpec represents a type declaration (TypeSpec production).
type TypeSpec struct {
	Name       *Ident        // type name
	TypeParams *FieldList    // type parameters; or nil (generics support)
	Assign     token.Pos     // position of '=', if any (type alias)
	Type       Expr          // *Ident, *ParenExpr, *SelectorExpr, *StarExpr, or any of the *XxxTypes
	Comment    *CommentGroup // line comments; or nil
}

func (s *TypeSpec) Pos() token.Pos { return s.Name.Pos() }
func (s *TypeSpec) End() token.Pos { return s.Type.End() }
func (s *TypeSpec) specNode()      {}
