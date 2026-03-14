package ast

import "github.com/matjam/plutonium/internal/token"

// ----------------------------------------------------------------------------
// Statements

// BadStmt is a placeholder for statements containing syntax errors for which
// no correct statement nodes can be created.
type BadStmt struct {
	From, To token.Pos // position range of bad statement
}

func (s *BadStmt) Pos() token.Pos { return s.From }
func (s *BadStmt) End() token.Pos { return s.To }
func (s *BadStmt) stmtNode()      {}

// DeclStmt represents a declaration in a statement list.
type DeclStmt struct {
	Decl Decl // *GenDecl with CONST, TYPE, or VAR token
}

func (s *DeclStmt) Pos() token.Pos { return s.Decl.Pos() }
func (s *DeclStmt) End() token.Pos { return s.Decl.End() }
func (s *DeclStmt) stmtNode()      {}

// EmptyStmt represents an empty statement.
// The "position" of the empty statement is the position of the immediately
// preceding semicolon.
type EmptyStmt struct {
	Semicolon token.Pos // position of preceding ";"
	Implicit  bool      // if set, ";" was omitted in the source
}

func (s *EmptyStmt) Pos() token.Pos { return s.Semicolon }
func (s *EmptyStmt) End() token.Pos {
	if s.Implicit {
		return s.Semicolon
	}
	return s.Semicolon + 1
}
func (s *EmptyStmt) stmtNode() {}

// LabeledStmt represents a labeled statement.
type LabeledStmt struct {
	Label *Ident
	Colon token.Pos
	Stmt  Stmt
}

func (s *LabeledStmt) Pos() token.Pos { return s.Label.Pos() }
func (s *LabeledStmt) End() token.Pos { return s.Stmt.End() }
func (s *LabeledStmt) stmtNode()      {}

// ExprStmt represents a (stand-alone) expression in a statement list.
type ExprStmt struct {
	X Expr // expression
}

func (s *ExprStmt) Pos() token.Pos { return s.X.Pos() }
func (s *ExprStmt) End() token.Pos { return s.X.End() }
func (s *ExprStmt) stmtNode()      {}

// SendStmt represents a send statement (chan <- value).
// This is a "parsed but forbidden" node — the parser produces it for good
// error messages but the semantic checker rejects it.
type SendStmt struct {
	Chan  Expr      // channel expression
	Arrow token.Pos // position of "<-"
	Value Expr      // value to send
}

func (s *SendStmt) Pos() token.Pos { return s.Chan.Pos() }
func (s *SendStmt) End() token.Pos { return s.Value.End() }
func (s *SendStmt) stmtNode()      {}

// IncDecStmt represents an increment or decrement statement.
type IncDecStmt struct {
	X      Expr
	TokPos token.Pos   // position of Tok
	Tok    token.Token // INC or DEC
}

func (s *IncDecStmt) Pos() token.Pos { return s.X.Pos() }
func (s *IncDecStmt) End() token.Pos {
	return token.Pos(int(s.TokPos) + len(s.Tok.String()))
}
func (s *IncDecStmt) stmtNode() {}

// AssignStmt represents an assignment statement (including := short variable declarations).
type AssignStmt struct {
	Lhs    []Expr
	TokPos token.Pos   // position of Tok
	Tok    token.Token // assignment token, DEFINE
	Rhs    []Expr
}

func (s *AssignStmt) Pos() token.Pos { return s.Lhs[0].Pos() }
func (s *AssignStmt) End() token.Pos { return s.Rhs[len(s.Rhs)-1].End() }
func (s *AssignStmt) stmtNode()      {}

// GoStmt represents a go statement (go func()).
// This is a "parsed but forbidden" node.
type GoStmt struct {
	Go   token.Pos // position of "go" keyword
	Call *CallExpr
}

func (s *GoStmt) Pos() token.Pos { return s.Go }
func (s *GoStmt) End() token.Pos { return s.Call.End() }
func (s *GoStmt) stmtNode()      {}

// ReturnStmt represents a return statement.
type ReturnStmt struct {
	Return  token.Pos // position of "return" keyword
	Results []Expr    // result expressions; or nil
}

func (s *ReturnStmt) Pos() token.Pos { return s.Return }
func (s *ReturnStmt) End() token.Pos {
	if n := len(s.Results); n > 0 {
		return s.Results[n-1].End()
	}
	return token.Pos(int(s.Return) + len("return"))
}
func (s *ReturnStmt) stmtNode() {}

// BranchStmt represents a break, continue, goto, or fallthrough statement.
type BranchStmt struct {
	TokPos token.Pos   // position of Tok
	Tok    token.Token // keyword token (BREAK, CONTINUE, GOTO, FALLTHROUGH)
	Label  *Ident      // label name; or nil
}

func (s *BranchStmt) Pos() token.Pos { return s.TokPos }
func (s *BranchStmt) End() token.Pos {
	if s.Label != nil {
		return s.Label.End()
	}
	return token.Pos(int(s.TokPos) + len(s.Tok.String()))
}
func (s *BranchStmt) stmtNode() {}

// BlockStmt represents a braced statement list.
type BlockStmt struct {
	Lbrace token.Pos // position of "{"
	List   []Stmt
	Rbrace token.Pos // position of "}", if block is non-empty
}

func (s *BlockStmt) Pos() token.Pos { return s.Lbrace }
func (s *BlockStmt) End() token.Pos {
	if s.Rbrace.IsValid() {
		return s.Rbrace + 1
	}
	if n := len(s.List); n > 0 {
		return s.List[n-1].End()
	}
	return s.Lbrace + 1
}
func (s *BlockStmt) stmtNode() {}

// IfStmt represents an if statement.
type IfStmt struct {
	If   token.Pos // position of "if" keyword
	Init Stmt      // initialization statement; or nil
	Cond Expr      // condition
	Body *BlockStmt
	Else Stmt // else branch; or nil
}

func (s *IfStmt) Pos() token.Pos { return s.If }
func (s *IfStmt) End() token.Pos {
	if s.Else != nil {
		return s.Else.End()
	}
	return s.Body.End()
}
func (s *IfStmt) stmtNode() {}

// CaseClause represents a case of an expression or type switch statement.
type CaseClause struct {
	Case  token.Pos // position of "case" or "default" keyword
	List  []Expr    // list of expressions or types; nil means default case
	Colon token.Pos // position of ":"
	Body  []Stmt    // statement list; or nil
}

func (s *CaseClause) Pos() token.Pos { return s.Case }
func (s *CaseClause) End() token.Pos {
	if n := len(s.Body); n > 0 {
		return s.Body[n-1].End()
	}
	return s.Colon + 1
}
func (s *CaseClause) stmtNode() {}

// SwitchStmt represents an expression switch statement.
type SwitchStmt struct {
	Switch token.Pos // position of "switch" keyword
	Init   Stmt      // initialization statement; or nil
	Tag    Expr      // tag expression; or nil
	Body   *BlockStmt
}

func (s *SwitchStmt) Pos() token.Pos { return s.Switch }
func (s *SwitchStmt) End() token.Pos { return s.Body.End() }
func (s *SwitchStmt) stmtNode()      {}

// TypeSwitchStmt represents a type switch statement.
type TypeSwitchStmt struct {
	Switch token.Pos // position of "switch" keyword
	Init   Stmt      // initialization statement; or nil
	Assign Stmt      // x := y.(type) or y.(type)
	Body   *BlockStmt
}

func (s *TypeSwitchStmt) Pos() token.Pos { return s.Switch }
func (s *TypeSwitchStmt) End() token.Pos { return s.Body.End() }
func (s *TypeSwitchStmt) stmtNode()      {}

// CommClause represents a case of a select statement.
// This is a "parsed but forbidden" node.
type CommClause struct {
	Case  token.Pos // position of "case" or "default" keyword
	Comm  Stmt      // send or receive statement; nil means default case
	Colon token.Pos // position of ":"
	Body  []Stmt    // statement list; or nil
}

func (s *CommClause) Pos() token.Pos { return s.Case }
func (s *CommClause) End() token.Pos {
	if n := len(s.Body); n > 0 {
		return s.Body[n-1].End()
	}
	return s.Colon + 1
}
func (s *CommClause) stmtNode() {}

// SelectStmt represents a select statement.
// This is a "parsed but forbidden" node.
type SelectStmt struct {
	Select token.Pos // position of "select" keyword
	Body   *BlockStmt
}

func (s *SelectStmt) Pos() token.Pos { return s.Select }
func (s *SelectStmt) End() token.Pos { return s.Body.End() }
func (s *SelectStmt) stmtNode()      {}

// ForStmt represents a for statement.
type ForStmt struct {
	For  token.Pos // position of "for" keyword
	Init Stmt      // initialization statement; or nil
	Cond Expr      // condition; or nil
	Post Stmt      // post iteration statement; or nil
	Body *BlockStmt
}

func (s *ForStmt) Pos() token.Pos { return s.For }
func (s *ForStmt) End() token.Pos { return s.Body.End() }
func (s *ForStmt) stmtNode()      {}

// RangeStmt represents a for statement with a range clause.
type RangeStmt struct {
	For    token.Pos   // position of "for" keyword
	Key    Expr        // Key may be nil
	Value  Expr        // Value may be nil
	TokPos token.Pos   // position of Tok; invalid if Key == nil
	Tok    token.Token // ILLEGAL if Key == nil, ASSIGN, DEFINE
	X      Expr        // value to range over
	Body   *BlockStmt
}

func (s *RangeStmt) Pos() token.Pos { return s.For }
func (s *RangeStmt) End() token.Pos { return s.Body.End() }
func (s *RangeStmt) stmtNode()      {}

// DeferStmt represents a defer statement.
type DeferStmt struct {
	Defer token.Pos // position of "defer" keyword
	Call  *CallExpr
}

func (s *DeferStmt) Pos() token.Pos { return s.Defer }
func (s *DeferStmt) End() token.Pos { return s.Call.End() }
func (s *DeferStmt) stmtNode()      {}
