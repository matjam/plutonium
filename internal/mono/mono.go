// Package mono implements generic monomorphization for Plutonium.
// It walks the type-checked AST, finds all calls to generic functions and uses
// of generic types with concrete type arguments, and produces a new package
// with all generics expanded into concrete instantiations. After this pass
// there are no type parameters in the AST.
package mono

import (
	"strings"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
	"github.com/matjam/plutonium/internal/types"
)

// FuncEntry is a concrete (non-generic) function after monomorphization.
type FuncEntry struct {
	Name string
	Decl *ast.FuncDecl
}

// Result is the output of monomorphization: a flat list of concrete functions.
type Result struct {
	Functions []*FuncEntry
	Files     []*ast.File // AST files for type declaration lookup
}

// Monomorphize takes a type-checked package and returns a Result with all
// generic instantiations expanded. The result contains no TypeParam nodes.
func Monomorphize(pkg *types.Package, fset *token.FileSet) (*Result, error) {
	m := &monomorphizer{
		fset:      fset,
		pkg:       pkg,
		instances: make(map[string]*ast.FuncDecl),
		result:    &Result{},
	}

	// Pass 1: collect all generic and non-generic functions
	var genericFuncs []*ast.FuncDecl
	var concreteFuncs []*ast.FuncDecl

	for _, file := range pkg.Files() {
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fd.Type.TypeParams != nil && len(fd.Type.TypeParams.List) > 0 {
					genericFuncs = append(genericFuncs, fd)
				} else {
					concreteFuncs = append(concreteFuncs, fd)
				}
			}
		}
	}

	// Pass 2: walk concrete functions to find generic instantiations
	for _, fd := range concreteFuncs {
		m.scanForInstantiations(fd)
	}

	// Pass 3: add concrete functions to result
	for _, fd := range concreteFuncs {
		name := fd.Name.Name
		// Methods: qualify with receiver type name (e.g. Counter.Inc)
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			if typeName := receiverTypeName(fd.Recv.List[0].Type); typeName != "" {
				name = typeName + "." + fd.Name.Name
			}
		}
		m.result.Functions = append(m.result.Functions, &FuncEntry{
			Name: name,
			Decl: fd,
		})
	}

	// Pass 4: emit monomorphized instances
	for name, decl := range m.instances {
		m.result.Functions = append(m.result.Functions, &FuncEntry{
			Name: name,
			Decl: decl,
		})
	}

	_ = genericFuncs // used for lookup by name

	// Include AST files for type declaration lookup by downstream passes.
	m.result.Files = pkg.Files()

	return m.result, nil
}

type monomorphizer struct {
	fset      *token.FileSet
	pkg       *types.Package
	instances map[string]*ast.FuncDecl // mangled name → concrete decl
	result    *Result
}

// scanForInstantiations walks a function looking for generic call expressions
// like Max[int32](a, b) and records the instantiation needed.
func (m *monomorphizer) scanForInstantiations(fd *ast.FuncDecl) {
	if fd.Body == nil {
		return
	}
	v := &instantiationScanner{m: m}
	ast.Walk(v, fd.Body)
}

type instantiationScanner struct {
	m *monomorphizer
}

func (s *instantiationScanner) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return s
	}
	// Look for IndexExpr: Max[int32](args)
	// Fun is *ast.IndexExpr where X is the function name and Index is the type arg
	idx, ok := call.Fun.(*ast.IndexExpr)
	if !ok {
		return s
	}
	funcName, ok := idx.X.(*ast.Ident)
	if !ok {
		return s
	}

	// Get the type argument name
	typeArg := typeArgName(idx.Index)
	if typeArg == "" {
		return s
	}

	mangledName := funcName.Name + "__" + sanitize(typeArg)

	// Find the generic function declaration
	genericDecl := s.m.findGenericFunc(funcName.Name)
	if genericDecl == nil {
		return s
	}

	// Create concrete instantiation if not already done
	if _, exists := s.m.instances[mangledName]; !exists {
		concrete := cloneFuncDecl(genericDecl, funcName.Name, mangledName, typeArg)
		s.m.instances[mangledName] = concrete
	}

	return s
}

func (m *monomorphizer) findGenericFunc(name string) *ast.FuncDecl {
	for _, file := range m.pkg.Files() {
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fd.Name.Name == name && fd.Type.TypeParams != nil && len(fd.Type.TypeParams.List) > 0 {
					return fd
				}
			}
		}
	}
	return nil
}

// receiverTypeName extracts the base type name from a receiver type expression.
// It handles both *T (pointer receiver) and T (value receiver).
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.PointerType:
		return receiverTypeName(e.X)
	}
	return ""
}

// typeArgName extracts a string name from a type argument expression.
func typeArgName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + typeArgName(e.X)
	case *ast.BasicLit:
		return e.Value
	}
	return ""
}

// sanitize turns a type name into a valid identifier component.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "*", "ptr_")
	s = strings.ReplaceAll(s, "[", "_slice_")
	s = strings.ReplaceAll(s, "]", "")
	return s
}

// cloneFuncDecl creates a concrete copy of a generic function with the
// type parameter replaced by a concrete type name.
func cloneFuncDecl(orig *ast.FuncDecl, origName, mangledName, typeArgName string) *ast.FuncDecl {
	// Build a type param substitution map
	subst := make(map[string]string)
	if orig.Type.TypeParams != nil {
		for _, field := range orig.Type.TypeParams.List {
			for _, name := range field.Names {
				subst[name.Name] = typeArgName
			}
		}
	}

	// Clone the function with substitutions
	clone := &ast.FuncDecl{
		Recv: orig.Recv,
		Name: &ast.Ident{NamePos: orig.Name.NamePos, Name: mangledName},
		Type: cloneFuncType(orig.Type, subst),
		Body: cloneBlock(orig.Body, subst),
	}
	return clone
}

func cloneFuncType(ft *ast.FuncType, subst map[string]string) *ast.FuncType {
	return &ast.FuncType{
		Func:       ft.Func,
		TypeParams: nil, // cleared — no type params in concrete version
		Params:     cloneFieldList(ft.Params, subst),
		Results:    cloneFieldList(ft.Results, subst),
	}
}

func cloneFieldList(fl *ast.FieldList, subst map[string]string) *ast.FieldList {
	if fl == nil {
		return nil
	}
	newFL := &ast.FieldList{Opening: fl.Opening, Closing: fl.Closing}
	for _, f := range fl.List {
		newFL.List = append(newFL.List, &ast.Field{
			Names:   f.Names,
			Type:    substituteType(f.Type, subst),
			Tag:     f.Tag,
			Comment: f.Comment,
		})
	}
	return newFL
}

// isIntLiteral reports whether s looks like an integer literal (e.g. "8", "256", "0xff").
func isIntLiteral(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			// Allow hex prefix and digits
			if r != 'x' && r != 'X' && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return false
			}
		}
	}
	return true
}

func substituteType(expr ast.Expr, subst map[string]string) ast.Expr {
	switch e := expr.(type) {
	case *ast.Ident:
		if replacement, ok := subst[e.Name]; ok {
			return &ast.Ident{NamePos: e.NamePos, Name: replacement}
		}
		return e
	case *ast.StarExpr:
		return &ast.StarExpr{Star: e.Star, X: substituteType(e.X, subst)}
	case *ast.ArrayType:
		return &ast.ArrayType{Lbrack: e.Lbrack, Len: e.Len, Elt: substituteType(e.Elt, subst)}
	case *ast.BinaryExpr:
		// Union constraint (T int32 | int64) — not a type expression in body
		return e
	}
	return expr
}

// substituteExpr replaces identifiers in expression positions with their
// substitutions. For const generic parameters (integer literals), the
// identifier is replaced with a BasicLit node.
func substituteExpr(expr ast.Expr, subst map[string]string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if replacement, ok := subst[e.Name]; ok {
			if isIntLiteral(replacement) {
				return &ast.BasicLit{
					ValuePos: e.NamePos,
					Kind:     token.INT,
					Value:    replacement,
				}
			}
			return &ast.Ident{NamePos: e.NamePos, Name: replacement}
		}
		return e
	case *ast.BasicLit:
		return e
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			X:     substituteExpr(e.X, subst),
			OpPos: e.OpPos,
			Op:    e.Op,
			Y:     substituteExpr(e.Y, subst),
		}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{
			OpPos: e.OpPos,
			Op:    e.Op,
			X:     substituteExpr(e.X, subst),
		}
	case *ast.ParenExpr:
		return &ast.ParenExpr{
			Lparen: e.Lparen,
			X:      substituteExpr(e.X, subst),
			Rparen: e.Rparen,
		}
	case *ast.CallExpr:
		args := make([]ast.Expr, len(e.Args))
		for i, a := range e.Args {
			args[i] = substituteExpr(a, subst)
		}
		return &ast.CallExpr{
			Fun:      substituteExpr(e.Fun, subst),
			Lparen:   e.Lparen,
			Args:     args,
			Ellipsis: e.Ellipsis,
			Rparen:   e.Rparen,
		}
	case *ast.IndexExpr:
		return &ast.IndexExpr{
			X:      substituteExpr(e.X, subst),
			Lbrack: e.Lbrack,
			Index:  substituteExpr(e.Index, subst),
			Rbrack: e.Rbrack,
		}
	case *ast.SelectorExpr:
		return &ast.SelectorExpr{
			X:   substituteExpr(e.X, subst),
			Sel: e.Sel,
		}
	case *ast.StarExpr:
		return &ast.StarExpr{
			Star: e.Star,
			X:    substituteExpr(e.X, subst),
		}
	case *ast.CompositeLit:
		elts := make([]ast.Expr, len(e.Elts))
		for i, elt := range e.Elts {
			elts[i] = substituteExpr(elt, subst)
		}
		return &ast.CompositeLit{
			Type:       e.Type,
			Lbrace:     e.Lbrace,
			Elts:       elts,
			Rbrace:     e.Rbrace,
			Incomplete: e.Incomplete,
		}
	case *ast.KeyValueExpr:
		return &ast.KeyValueExpr{
			Key:   substituteExpr(e.Key, subst),
			Colon: e.Colon,
			Value: substituteExpr(e.Value, subst),
		}
	}
	return expr
}

func cloneBlock(block *ast.BlockStmt, subst map[string]string) *ast.BlockStmt {
	if block == nil {
		return nil
	}
	newBlock := &ast.BlockStmt{Lbrace: block.Lbrace, Rbrace: block.Rbrace}
	for _, stmt := range block.List {
		newBlock.List = append(newBlock.List, cloneStmt(stmt, subst))
	}
	return newBlock
}

func cloneStmt(stmt ast.Stmt, subst map[string]string) ast.Stmt {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		results := make([]ast.Expr, len(s.Results))
		for i, r := range s.Results {
			results[i] = substituteExpr(r, subst)
		}
		return &ast.ReturnStmt{Return: s.Return, Results: results}
	case *ast.ExprStmt:
		return &ast.ExprStmt{X: substituteExpr(s.X, subst)}
	case *ast.AssignStmt:
		lhs := make([]ast.Expr, len(s.Lhs))
		for i, l := range s.Lhs {
			lhs[i] = substituteExpr(l, subst)
		}
		rhs := make([]ast.Expr, len(s.Rhs))
		for i, r := range s.Rhs {
			rhs[i] = substituteExpr(r, subst)
		}
		return &ast.AssignStmt{
			Lhs:    lhs,
			TokPos: s.TokPos,
			Tok:    s.Tok,
			Rhs:    rhs,
		}
	case *ast.IfStmt:
		return &ast.IfStmt{
			If:   s.If,
			Init: cloneStmt(s.Init, subst),
			Cond: substituteExpr(s.Cond, subst),
			Body: cloneBlock(s.Body, subst),
			Else: cloneStmt(s.Else, subst),
		}
	case *ast.ForStmt:
		return &ast.ForStmt{
			For:  s.For,
			Init: cloneStmt(s.Init, subst),
			Cond: substituteExpr(s.Cond, subst),
			Post: cloneStmt(s.Post, subst),
			Body: cloneBlock(s.Body, subst),
		}
	case *ast.BlockStmt:
		return cloneBlock(s, subst)
	case *ast.IncDecStmt:
		return &ast.IncDecStmt{
			X:      substituteExpr(s.X, subst),
			TokPos: s.TokPos,
			Tok:    s.Tok,
		}
	case *ast.DeclStmt:
		return s // declaration statements don't need expression substitution for now
	case *ast.BranchStmt:
		return s
	case *ast.EmptyStmt:
		return s
	case *ast.SwitchStmt:
		return &ast.SwitchStmt{
			Switch: s.Switch,
			Init:   cloneStmt(s.Init, subst),
			Tag:    substituteExpr(s.Tag, subst),
			Body:   cloneBlock(s.Body, subst),
		}
	case *ast.CaseClause:
		list := make([]ast.Expr, len(s.List))
		for i, e := range s.List {
			list[i] = substituteExpr(e, subst)
		}
		body := make([]ast.Stmt, len(s.Body))
		for i, st := range s.Body {
			body[i] = cloneStmt(st, subst)
		}
		return &ast.CaseClause{
			Case:  s.Case,
			List:  list,
			Colon: s.Colon,
			Body:  body,
		}
	}
	// Fallback for unhandled statement types
	return stmt
}
