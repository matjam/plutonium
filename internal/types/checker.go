package types

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

// Info stores type information collected during type checking.
type Info struct {
	// Types maps expressions to their types.
	Types map[ast.Expr]Type
	// Defs maps identifiers to the objects they define.
	Defs map[*ast.Ident]*Object
	// Uses maps identifiers to the objects they refer to.
	Uses map[*ast.Ident]*Object
}

// NewInfo creates a new Info with initialized maps.
func NewInfo() *Info {
	return &Info{
		Types: make(map[ast.Expr]Type),
		Defs:  make(map[*ast.Ident]*Object),
		Uses:  make(map[*ast.Ident]*Object),
	}
}

// Check type-checks a package consisting of the given files.
// fset is used for position information.
// info may be nil; if non-nil, it is populated with type information.
// Returns the package and any type errors.
func Check(pkgPath string, fset *token.FileSet, files []*ast.File, info *Info) (*Package, error) {
	if info == nil {
		info = NewInfo()
	}
	chk := &checker{
		fset:   fset,
		info:   info,
		errors: nil,
	}

	pkg := &Package{
		path:  pkgPath,
		scope: NewScope(Universe),
	}
	if len(files) > 0 {
		pkg.name = files[0].Name.Name
	}
	pkg.files = files

	chk.pkg = pkg

	// Phase 1: collect all top-level declarations
	for _, file := range files {
		chk.collectFileDecls(file, pkg.scope)
	}

	// Phase 2: check forbidden imports
	for _, file := range files {
		for _, imp := range file.Imports {
			path := imp.Path.Value
			// strip surrounding quotes
			if len(path) >= 2 {
				path = path[1 : len(path)-1]
			}
			if path == "reflect" {
				chk.errorf(imp.Path.Pos(), "import of 'reflect' is forbidden in Plutonium (requires runtime metadata)")
			}
		}
	}

	// Phase 3: type-check all declarations
	for _, file := range files {
		chk.checkFile(file)
	}

	if len(chk.errors) > 0 {
		return pkg, chk.errors
	}
	return pkg, nil
}

// checker holds the state for a single type-checking pass.
type checker struct {
	fset   *token.FileSet
	pkg    *Package
	info   *Info
	errors errorList
	scope  *Scope // current scope (nil = use package scope)
}

func (c *checker) errorf(pos token.Pos, format string, args ...interface{}) {
	c.errors.add(c.fset.Position(pos), fmt.Sprintf(format, args...))
}

func (c *checker) currentScope() *Scope {
	if c.scope != nil {
		return c.scope
	}
	return c.pkg.scope
}

// collectFileDecls collects top-level declarations into the package scope
// so that forward references work correctly.
func (c *checker) collectFileDecls(file *ast.File, scope *Scope) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			obj := &Object{Kind: FunObj, Name: d.Name.Name, Pos: d.Name.Pos()}
			scope.Insert(obj)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					obj := &Object{Kind: TypObj, Name: s.Name.Name, Pos: s.Name.Pos()}
					scope.Insert(obj)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						kind := VarObj
						if d.Tok == token.CONST {
							kind = ConObj
						}
						obj := &Object{Kind: kind, Name: name.Name, Pos: name.Pos()}
						scope.Insert(obj)
					}
				}
			}
		}
	}
}

// checkFile type-checks all declarations in a file.
func (c *checker) checkFile(file *ast.File) {
	for _, decl := range file.Decls {
		c.checkDecl(decl)
	}
}

func (c *checker) checkDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		c.checkFuncDecl(d)
	case *ast.GenDecl:
		c.checkGenDecl(d)
	case *ast.BadDecl:
		// already errored during parsing
	}
}

func (c *checker) checkFuncDecl(d *ast.FuncDecl) {
	// Open function scope
	prevScope := c.scope
	c.scope = NewScope(c.pkg.scope)
	defer func() { c.scope = prevScope }()

	// Add receiver to scope
	if d.Recv != nil {
		for _, field := range d.Recv.List {
			typ := c.resolveType(field.Type)
			for _, name := range field.Names {
				obj := &Object{Kind: VarObj, Name: name.Name, Type: typ, Pos: name.Pos()}
				c.scope.Insert(obj)
				c.info.Defs[name] = obj
			}
		}
	}

	// Add parameters to scope
	if d.Type.Params != nil {
		for _, field := range d.Type.Params.List {
			typ := c.resolveType(field.Type)
			for _, name := range field.Names {
				obj := &Object{Kind: VarObj, Name: name.Name, Type: typ, Pos: name.Pos()}
				c.scope.Insert(obj)
				c.info.Defs[name] = obj
			}
		}
	}

	// Add named return values to scope
	if d.Type.Results != nil {
		for _, field := range d.Type.Results.List {
			typ := c.resolveType(field.Type)
			for _, name := range field.Names {
				obj := &Object{Kind: VarObj, Name: name.Name, Type: typ, Pos: name.Pos()}
				c.scope.Insert(obj)
				c.info.Defs[name] = obj
			}
		}
	}

	// Check the body (if not an assembly stub)
	if d.Body != nil {
		c.checkBlock(d.Body)
	}
}

func (c *checker) checkGenDecl(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			c.checkValueSpec(s, d.Tok == token.CONST)
		case *ast.TypeSpec:
			c.checkTypeSpec(s)
		case *ast.ImportSpec:
			// imports handled in phase 2
		}
	}
}

func (c *checker) checkValueSpec(s *ast.ValueSpec, isConst bool) {
	var specType Type
	if s.Type != nil {
		specType = c.resolveType(s.Type)
	}
	for i, name := range s.Names {
		var valType Type
		if i < len(s.Values) {
			valType = c.checkExpr(s.Values[i])
		}
		typ := specType
		if typ == nil {
			typ = valType
		}
		kind := VarObj
		if isConst {
			kind = ConObj
		}
		obj := &Object{Kind: kind, Name: name.Name, Type: typ, Pos: name.Pos()}
		// For top-level, the object was already pre-inserted; update its type.
		if existing := c.currentScope().LookupLocal(name.Name); existing != nil {
			existing.Type = typ
		} else {
			c.currentScope().Insert(obj)
		}
		c.info.Defs[name] = obj
	}
}

func (c *checker) checkTypeSpec(s *ast.TypeSpec) {
	// resolve the underlying type
	underlying := c.resolveType(s.Type)
	typeName := &TypeName{name: s.Name.Name}
	named := &Named{obj: typeName, underlying: underlying}
	typeName.typ = named
	obj := &Object{Kind: TypObj, Name: s.Name.Name, Type: named, Pos: s.Name.Pos()}
	// Update the forward-declared object or insert fresh
	if existing := c.currentScope().LookupLocal(s.Name.Name); existing != nil {
		existing.Type = named
	} else {
		c.currentScope().Insert(obj)
	}
	c.info.Defs[s.Name] = obj
}

func (c *checker) checkBlock(block *ast.BlockStmt) {
	prevScope := c.scope
	c.scope = NewScope(prevScope)
	defer func() { c.scope = prevScope }()
	for _, stmt := range block.List {
		c.checkStmt(stmt)
	}
}

func (c *checker) checkStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		c.checkExpr(s.X)
	case *ast.AssignStmt:
		c.checkAssign(s)
	case *ast.ReturnStmt:
		for _, r := range s.Results {
			c.checkExpr(r)
		}
	case *ast.IfStmt:
		if s.Init != nil {
			c.checkStmt(s.Init)
		}
		c.checkExpr(s.Cond)
		c.checkBlock(s.Body)
		if s.Else != nil {
			c.checkStmt(s.Else)
		}
	case *ast.ForStmt:
		prevScope := c.scope
		c.scope = NewScope(prevScope)
		defer func() { c.scope = prevScope }()
		if s.Init != nil {
			c.checkStmt(s.Init)
		}
		if s.Cond != nil {
			c.checkExpr(s.Cond)
		}
		if s.Post != nil {
			c.checkStmt(s.Post)
		}
		c.checkBlock(s.Body)
	case *ast.RangeStmt:
		c.checkExpr(s.X)
		c.checkBlock(s.Body)
	case *ast.BlockStmt:
		c.checkBlock(s)
	case *ast.DeclStmt:
		c.checkDecl(s.Decl)
	case *ast.IncDecStmt:
		c.checkExpr(s.X)
	case *ast.BranchStmt:
		// break, continue, goto, fallthrough — nothing to check
	case *ast.DeferStmt:
		c.checkExpr(s.Call)
	case *ast.SwitchStmt:
		if s.Init != nil {
			c.checkStmt(s.Init)
		}
		if s.Tag != nil {
			c.checkExpr(s.Tag)
		}
		c.checkBlock(s.Body)
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			c.checkStmt(s.Init)
		}
		c.checkBlock(s.Body)
	case *ast.CaseClause:
		for _, x := range s.List {
			c.checkExpr(x)
		}
		for _, st := range s.Body {
			c.checkStmt(st)
		}
	case *ast.EmptyStmt:
		// nothing
	case *ast.LabeledStmt:
		c.checkStmt(s.Stmt)
	case *ast.BadStmt:
		// already errored
	case *ast.GoStmt:
		c.errorf(s.Go, "goroutines are not permitted in Plutonium")
	case *ast.SelectStmt:
		c.errorf(s.Select, "select is not permitted in Plutonium")
	case *ast.SendStmt:
		c.errorf(s.Arrow, "channel send is not permitted in Plutonium")
	}
}

func (c *checker) checkAssign(s *ast.AssignStmt) {
	// Check RHS types
	rhsTypes := make([]Type, len(s.Rhs))
	for i, rhs := range s.Rhs {
		rhsTypes[i] = c.checkExpr(rhs)
	}

	if s.Tok == token.DEFINE {
		// Short variable declaration :=
		for i, lhs := range s.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok {
				c.errorf(lhs.Pos(), "non-name on left side of :=")
				continue
			}
			if ident.Name == "_" {
				continue
			}
			var typ Type
			if i < len(rhsTypes) {
				typ = rhsTypes[i]
			}
			obj := &Object{Kind: VarObj, Name: ident.Name, Type: typ, Pos: ident.Pos()}
			// Shadowing in inner scope is OK
			c.currentScope().objects[ident.Name] = obj
			c.info.Defs[ident] = obj
		}
	} else {
		// Regular assignment =, +=, etc.
		for _, lhs := range s.Lhs {
			c.checkExpr(lhs)
		}
	}
}
