package ast

import "fmt"

// Visitor is the interface that wraps the Visit method.
//
// Visit is called for each node during a Walk. If the result visitor w is not
// nil, Walk visits each of the children of node with the visitor w, followed
// by a call of w.Visit(nil).
type Visitor interface {
	Visit(node Node) (w Visitor)
}

// Walk traverses an AST in depth-first order: It starts by calling
// v.Visit(node); node must not be nil. If the visitor w returned by
// v.Visit(node) is not nil, Walk is invoked recursively with visitor w
// for each of the non-nil children of node, followed by a call of
// w.Visit(nil).
func Walk(v Visitor, node Node) {
	if v = v.Visit(node); v == nil {
		return
	}

	// walk children
	switch n := node.(type) {

	// File
	case *File:
		Walk(v, n.Name)
		for _, d := range n.Decls {
			Walk(v, d)
		}

	// Declarations
	case *BadDecl:
		// leaf

	case *GenDecl:
		for _, s := range n.Specs {
			Walk(v, s)
		}

	case *FuncDecl:
		if n.Recv != nil {
			Walk(v, n.Recv)
		}
		Walk(v, n.Name)
		Walk(v, n.Type)
		if n.Body != nil {
			Walk(v, n.Body)
		}

	// Specs
	case *ImportSpec:
		if n.Name != nil {
			Walk(v, n.Name)
		}
		Walk(v, n.Path)

	case *ValueSpec:
		for _, name := range n.Names {
			Walk(v, name)
		}
		if n.Type != nil {
			Walk(v, n.Type)
		}
		for _, val := range n.Values {
			Walk(v, val)
		}

	case *TypeSpec:
		Walk(v, n.Name)
		if n.TypeParams != nil {
			Walk(v, n.TypeParams)
		}
		Walk(v, n.Type)

	// Expressions
	case *Ident:
		// leaf

	case *BasicLit:
		// leaf

	case *CompositeLit:
		if n.Type != nil {
			Walk(v, n.Type)
		}
		for _, e := range n.Elts {
			Walk(v, e)
		}

	case *FuncLit:
		Walk(v, n.Type)
		Walk(v, n.Body)

	case *ParenExpr:
		Walk(v, n.X)

	case *SelectorExpr:
		Walk(v, n.X)
		Walk(v, n.Sel)

	case *IndexExpr:
		Walk(v, n.X)
		Walk(v, n.Index)

	case *SliceExpr:
		Walk(v, n.X)
		if n.Low != nil {
			Walk(v, n.Low)
		}
		if n.High != nil {
			Walk(v, n.High)
		}
		if n.Max != nil {
			Walk(v, n.Max)
		}

	case *TypeAssertExpr:
		Walk(v, n.X)
		if n.Type != nil {
			Walk(v, n.Type)
		}

	case *CallExpr:
		Walk(v, n.Fun)
		for _, a := range n.Args {
			Walk(v, a)
		}

	case *StarExpr:
		Walk(v, n.X)

	case *UnaryExpr:
		Walk(v, n.X)

	case *BinaryExpr:
		Walk(v, n.X)
		Walk(v, n.Y)

	case *KeyValueExpr:
		Walk(v, n.Key)
		Walk(v, n.Value)

	// Type nodes
	case *ArrayType:
		if n.Len != nil {
			Walk(v, n.Len)
		}
		Walk(v, n.Elt)

	case *StructType:
		Walk(v, n.Fields)

	case *FuncType:
		if n.TypeParams != nil {
			Walk(v, n.TypeParams)
		}
		Walk(v, n.Params)
		if n.Results != nil {
			Walk(v, n.Results)
		}

	case *InterfaceType:
		Walk(v, n.Methods)

	case *PointerType:
		Walk(v, n.X)

	case *PackedType:
		if n.BackingType != nil {
			Walk(v, n.BackingType)
		}
		Walk(v, n.Fields)

	case *BitsType:
		if n.AccessType != nil {
			Walk(v, n.AccessType)
		}
		if n.Width != nil {
			Walk(v, n.Width)
		}

	case *BitsGroupType:
		for _, name := range n.Names {
			Walk(v, name)
		}

	case *Ellipsis:
		Walk(v, n.Elt)

	// FieldList and Field
	case *FieldList:
		for _, f := range n.List {
			Walk(v, f)
		}

	case *Field:
		for _, name := range n.Names {
			Walk(v, name)
		}
		Walk(v, n.Type)
		if n.Tag != nil {
			Walk(v, n.Tag)
		}

	// Statements
	case *BadStmt:
		// leaf

	case *DeclStmt:
		Walk(v, n.Decl)

	case *EmptyStmt:
		// leaf

	case *LabeledStmt:
		Walk(v, n.Label)
		Walk(v, n.Stmt)

	case *ExprStmt:
		Walk(v, n.X)

	case *SendStmt:
		Walk(v, n.Chan)
		Walk(v, n.Value)

	case *IncDecStmt:
		Walk(v, n.X)

	case *AssignStmt:
		for _, x := range n.Lhs {
			Walk(v, x)
		}
		for _, x := range n.Rhs {
			Walk(v, x)
		}

	case *GoStmt:
		Walk(v, n.Call)

	case *ReturnStmt:
		for _, x := range n.Results {
			Walk(v, x)
		}

	case *BranchStmt:
		if n.Label != nil {
			Walk(v, n.Label)
		}

	case *BlockStmt:
		for _, s := range n.List {
			Walk(v, s)
		}

	case *IfStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		Walk(v, n.Cond)
		Walk(v, n.Body)
		if n.Else != nil {
			Walk(v, n.Else)
		}

	case *CaseClause:
		for _, x := range n.List {
			Walk(v, x)
		}
		for _, s := range n.Body {
			Walk(v, s)
		}

	case *SwitchStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		if n.Tag != nil {
			Walk(v, n.Tag)
		}
		Walk(v, n.Body)

	case *TypeSwitchStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		Walk(v, n.Assign)
		Walk(v, n.Body)

	case *CommClause:
		if n.Comm != nil {
			Walk(v, n.Comm)
		}
		for _, s := range n.Body {
			Walk(v, s)
		}

	case *SelectStmt:
		Walk(v, n.Body)

	case *ForStmt:
		if n.Init != nil {
			Walk(v, n.Init)
		}
		if n.Cond != nil {
			Walk(v, n.Cond)
		}
		if n.Post != nil {
			Walk(v, n.Post)
		}
		Walk(v, n.Body)

	case *RangeStmt:
		if n.Key != nil {
			Walk(v, n.Key)
		}
		if n.Value != nil {
			Walk(v, n.Value)
		}
		Walk(v, n.X)
		Walk(v, n.Body)

	case *DeferStmt:
		Walk(v, n.Call)

	default:
		panic(fmt.Sprintf("ast.Walk: unexpected node type %T", n))
	}

	v.Visit(nil)
}
