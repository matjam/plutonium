package ast

import "github.com/matjam/plutonium/internal/token"

// ----------------------------------------------------------------------------
// Identifier

// Ident represents an identifier.
type Ident struct {
	NamePos token.Pos // identifier position
	Name    string    // identifier name
	Obj     *Object   // denoted object; or nil
}

func (x *Ident) Pos() token.Pos { return x.NamePos }
func (x *Ident) End() token.Pos { return token.Pos(int(x.NamePos) + len(x.Name)) }
func (x *Ident) exprNode()      {}
func (x *Ident) String() string { return x.Name }

// ----------------------------------------------------------------------------
// Basic literal

// BasicLit represents a literal of basic type.
type BasicLit struct {
	ValuePos token.Pos   // literal position
	Kind     token.Token // token.INT, token.FLOAT, token.IMAG, token.CHAR, or token.STRING
	Value    string      // literal string; e.g. 42, 0x7f, 3.14, 1e-9, 2.4i, 'a', '\x7f', "foo" or `\m\n\o`
}

func (x *BasicLit) Pos() token.Pos { return x.ValuePos }
func (x *BasicLit) End() token.Pos { return token.Pos(int(x.ValuePos) + len(x.Value)) }
func (x *BasicLit) exprNode()      {}

// ----------------------------------------------------------------------------
// Composite literal

// CompositeLit represents a composite literal expression.
type CompositeLit struct {
	Type       Expr      // literal type; or nil
	Lbrace     token.Pos // position of "{"
	Elts       []Expr    // list of composite elements; or nil
	Rbrace     token.Pos // position of "}"
	Incomplete bool      // true if (source) expressions are missing in the Elts list
}

func (x *CompositeLit) Pos() token.Pos {
	if x.Type != nil {
		return x.Type.Pos()
	}
	return x.Lbrace
}
func (x *CompositeLit) End() token.Pos { return x.Rbrace + 1 }
func (x *CompositeLit) exprNode()      {}

// ----------------------------------------------------------------------------
// Function literal

// FuncLit represents a function literal expression.
type FuncLit struct {
	Type *FuncType  // function type
	Body *BlockStmt // function body
}

func (x *FuncLit) Pos() token.Pos { return x.Type.Pos() }
func (x *FuncLit) End() token.Pos { return x.Body.End() }
func (x *FuncLit) exprNode()      {}

// ----------------------------------------------------------------------------
// Parenthesized expression

// ParenExpr represents a parenthesized expression.
type ParenExpr struct {
	Lparen token.Pos // position of "("
	X      Expr      // parenthesized expression
	Rparen token.Pos // position of ")"
}

func (x *ParenExpr) Pos() token.Pos { return x.Lparen }
func (x *ParenExpr) End() token.Pos { return x.Rparen + 1 }
func (x *ParenExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Selector expression

// SelectorExpr represents an expression followed by a selector.
type SelectorExpr struct {
	X   Expr   // expression
	Sel *Ident // field selector
}

func (x *SelectorExpr) Pos() token.Pos { return x.X.Pos() }
func (x *SelectorExpr) End() token.Pos { return x.Sel.End() }
func (x *SelectorExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Index expression

// IndexExpr represents an expression followed by an index.
type IndexExpr struct {
	X      Expr      // expression
	Lbrack token.Pos // position of "["
	Index  Expr      // index expression
	Rbrack token.Pos // position of "]"
}

func (x *IndexExpr) Pos() token.Pos { return x.X.Pos() }
func (x *IndexExpr) End() token.Pos { return x.Rbrack + 1 }
func (x *IndexExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Slice expression

// SliceExpr represents an expression followed by slice indices.
type SliceExpr struct {
	X      Expr      // expression
	Lbrack token.Pos // position of "["
	Low    Expr      // begin of slice range; or nil
	High   Expr      // end of slice range; or nil
	Max    Expr      // maximum capacity of slice; or nil (only valid for 3-index slices)
	Slice3 bool      // true if 3-index slice (2 colons present)
	Rbrack token.Pos // position of "]"
}

func (x *SliceExpr) Pos() token.Pos { return x.X.Pos() }
func (x *SliceExpr) End() token.Pos { return x.Rbrack + 1 }
func (x *SliceExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Type assertion expression

// TypeAssertExpr represents an expression followed by a type assertion.
type TypeAssertExpr struct {
	X      Expr      // expression
	Lparen token.Pos // position of "("
	Type   Expr      // asserted type; nil means type switch X.(type)
	Rparen token.Pos // position of ")"
}

func (x *TypeAssertExpr) Pos() token.Pos { return x.X.Pos() }
func (x *TypeAssertExpr) End() token.Pos { return x.Rparen + 1 }
func (x *TypeAssertExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Call expression

// CallExpr represents an expression followed by an argument list.
type CallExpr struct {
	Fun      Expr      // function expression
	Lparen   token.Pos // position of "("
	Args     []Expr    // function arguments; or nil
	Ellipsis token.Pos // position of "..." (token.NoPos if there is no "...")
	Rparen   token.Pos // position of ")"
}

func (x *CallExpr) Pos() token.Pos { return x.Fun.Pos() }
func (x *CallExpr) End() token.Pos { return x.Rparen + 1 }
func (x *CallExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Star expression

// StarExpr represents an expression of the form "*" Expression.
type StarExpr struct {
	Star token.Pos // position of "*"
	X    Expr      // operand
}

func (x *StarExpr) Pos() token.Pos { return x.Star }
func (x *StarExpr) End() token.Pos { return x.X.End() }
func (x *StarExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Unary expression

// UnaryExpr represents a unary expression.
type UnaryExpr struct {
	OpPos token.Pos   // position of Op
	Op    token.Token // operator
	X     Expr        // operand
}

func (x *UnaryExpr) Pos() token.Pos { return x.OpPos }
func (x *UnaryExpr) End() token.Pos { return x.X.End() }
func (x *UnaryExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Binary expression

// BinaryExpr represents a binary expression.
type BinaryExpr struct {
	X     Expr        // left operand
	OpPos token.Pos   // position of Op
	Op    token.Token // operator
	Y     Expr        // right operand
}

func (x *BinaryExpr) Pos() token.Pos { return x.X.Pos() }
func (x *BinaryExpr) End() token.Pos { return x.Y.End() }
func (x *BinaryExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Key-value expression

// KeyValueExpr represents (key : value) pairs in composite literals.
type KeyValueExpr struct {
	Key   Expr      // key expression
	Colon token.Pos // position of ":"
	Value Expr      // value expression
}

func (x *KeyValueExpr) Pos() token.Pos { return x.Key.Pos() }
func (x *KeyValueExpr) End() token.Pos { return x.Value.End() }
func (x *KeyValueExpr) exprNode()      {}

// ----------------------------------------------------------------------------
// Type nodes

// ArrayType represents an array or slice type.
type ArrayType struct {
	Lbrack token.Pos // position of "["
	Len    Expr      // Ellipsis node for [...]T array types, nil for slice types
	Elt    Expr      // element type
}

func (x *ArrayType) Pos() token.Pos { return x.Lbrack }
func (x *ArrayType) End() token.Pos { return x.Elt.End() }
func (x *ArrayType) exprNode()      {}

// StructType represents a struct type.
type StructType struct {
	Struct     token.Pos  // position of "struct" keyword
	Fields     *FieldList // list of field declarations
	Incomplete bool       // true if (source) fields are missing in the Fields list
}

func (x *StructType) Pos() token.Pos { return x.Struct }
func (x *StructType) End() token.Pos { return x.Fields.End() }
func (x *StructType) exprNode()      {}

// FuncType represents a function type.
type FuncType struct {
	Func       token.Pos  // position of "func" keyword (token.NoPos if there is no "func")
	TypeParams *FieldList // type parameters; or nil (generics support)
	Params     *FieldList // (incoming) parameters; non-nil
	Results    *FieldList // (outgoing) results; or nil
}

func (x *FuncType) Pos() token.Pos {
	if x.Func.IsValid() {
		return x.Func
	}
	return x.Params.Pos()
}
func (x *FuncType) End() token.Pos {
	if x.Results != nil {
		return x.Results.End()
	}
	return x.Params.End()
}
func (x *FuncType) exprNode() {}

// InterfaceType represents an interface type.
type InterfaceType struct {
	Interface  token.Pos  // position of "interface" keyword
	Methods    *FieldList // list of embedded interfaces, methods, or types
	Incomplete bool       // true if (source) methods are missing in the Methods list
}

func (x *InterfaceType) Pos() token.Pos { return x.Interface }
func (x *InterfaceType) End() token.Pos { return x.Methods.End() }
func (x *InterfaceType) exprNode()      {}

// PointerType represents a pointer type.
type PointerType struct {
	Star token.Pos // position of "*"
	X    Expr      // pointer base type
}

func (x *PointerType) Pos() token.Pos { return x.Star }
func (x *PointerType) End() token.Pos { return x.X.End() }
func (x *PointerType) exprNode()      {}

// ----------------------------------------------------------------------------
// Field and FieldList

// Field represents a Field declaration in a struct type, a method in an
// interface type, or a parameter/result declaration in a signature.
type Field struct {
	Names   []*Ident      // field/method/parameter names; or nil
	Type    Expr          // field/method/parameter type
	Tag     *BasicLit     // field tag; or nil
	Comment *CommentGroup // line comments; or nil
}

func (f *Field) Pos() token.Pos {
	if len(f.Names) > 0 {
		return f.Names[0].Pos()
	}
	return f.Type.Pos()
}

func (f *Field) End() token.Pos {
	if f.Tag != nil {
		return f.Tag.End()
	}
	return f.Type.End()
}

// FieldList represents a list of Fields, enclosed by parentheses,
// curly braces, or square brackets.
type FieldList struct {
	Opening token.Pos // position of opening parenthesis/bracket, if any
	List    []*Field  // field list; or nil
	Closing token.Pos // position of closing parenthesis/bracket, if any
}

func (f *FieldList) Pos() token.Pos {
	if f.Opening.IsValid() {
		return f.Opening
	}
	if len(f.List) > 0 {
		return f.List[0].Pos()
	}
	return token.NoPos
}

func (f *FieldList) End() token.Pos {
	if f.Closing.IsValid() {
		return f.Closing + 1
	}
	if n := len(f.List); n > 0 {
		return f.List[n-1].End()
	}
	return token.NoPos
}

// NumFields returns the number of parameters or struct fields represented by a FieldList.
func (f *FieldList) NumFields() int {
	n := 0
	if f != nil {
		for _, g := range f.List {
			m := len(g.Names)
			if m == 0 {
				m = 1 // anonymous field
			}
			n += m
		}
	}
	return n
}
