package types

import (
	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

// checkExpr infers and checks the type of an expression.
// Returns the type, or Typ[Invalid] on error.
func (c *checker) checkExpr(expr ast.Expr) Type {
	if expr == nil {
		return Typ[Invalid]
	}
	typ := c.checkExprInternal(expr)
	if typ != nil {
		c.info.Types[expr] = typ
	}
	return typ
}

func (c *checker) checkExprInternal(expr ast.Expr) Type {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return c.checkBasicLit(e)

	case *ast.Ident:
		return c.checkIdent(e)

	case *ast.BinaryExpr:
		return c.checkBinaryExpr(e)

	case *ast.UnaryExpr:
		return c.checkUnaryExpr(e)

	case *ast.CallExpr:
		return c.checkCallExpr(e)

	case *ast.SelectorExpr:
		return c.checkSelectorExpr(e)

	case *ast.IndexExpr:
		return c.checkIndexExpr(e)

	case *ast.SliceExpr:
		// Returns same slice type
		xt := c.checkExpr(e.X)
		if e.Low != nil {
			c.checkExpr(e.Low)
		}
		if e.High != nil {
			c.checkExpr(e.High)
		}
		if e.Max != nil {
			c.checkExpr(e.Max)
		}
		return xt

	case *ast.StarExpr:
		xt := c.checkExpr(e.X)
		if pt, ok := xt.(*Pointer); ok {
			return pt.base
		}
		// Could be a type expression like *int32 used in value position
		return xt

	case *ast.ParenExpr:
		return c.checkExpr(e.X)

	case *ast.CompositeLit:
		return c.checkCompositeLit(e)

	case *ast.FuncLit:
		// Closures are allowed as long as captures don't escape to heap
		// For v0.1, just type-check the body
		if e.Body != nil {
			c.checkBlock(e.Body)
		}
		return c.resolveType(e.Type)

	case *ast.TypeAssertExpr:
		c.errorf(e.X.Pos(), "type assertions on interfaces are not permitted in Plutonium")
		return Typ[Invalid]

	case *ast.KeyValueExpr:
		// Key in a KeyValueExpr is typically a struct field name (identifier).
		// Only check the key if it's not a plain identifier (field names don't need scope lookup).
		if _, isIdent := e.Key.(*ast.Ident); !isIdent {
			c.checkExpr(e.Key)
		}
		return c.checkExpr(e.Value)

	// Type expressions used in calls like int32(x), or in composite literals
	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.PointerType:
		return c.resolveType(expr)
	}

	return Typ[Invalid]
}

func (c *checker) checkBasicLit(e *ast.BasicLit) Type {
	switch e.Kind {
	case token.INT:
		return Typ[Int] // untyped int constant
	case token.FLOAT:
		return Typ[Float64]
	case token.CHAR:
		return Typ[Rune]
	case token.STRING:
		return Typ[String]
	}
	return Typ[Invalid]
}

func (c *checker) checkIdent(e *ast.Ident) Type {
	if e.Name == "_" {
		return Typ[Invalid]
	}

	// Reject 'any' type usage explicitly
	if e.Name == "any" {
		c.errorf(e.Pos(), "'any' (empty interface) is not permitted in Plutonium")
		return Typ[Invalid]
	}

	obj := c.currentScope().Lookup(e.Name)
	if obj == nil {
		c.errorf(e.Pos(), "undefined: %s", e.Name)
		return Typ[Invalid]
	}
	c.info.Uses[e] = obj

	// Reject forbidden built-in identifiers (when used outside call context)
	if obj.Kind == BuiltinObj {
		if b, ok := obj.Type.(*Builtin); ok && b.forbidden {
			c.errorf(e.Pos(), "built-in %q is forbidden in Plutonium", e.Name)
			return Typ[Invalid]
		}
	}

	return obj.Type
}

func (c *checker) checkBinaryExpr(e *ast.BinaryExpr) Type {
	xt := c.checkExpr(e.X)
	yt := c.checkExpr(e.Y)
	// For comparison operators, result is bool
	switch e.Op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return Typ[Bool]
	case token.LAND, token.LOR:
		return Typ[Bool]
	}
	// For arithmetic, use the left operand type (simplified)
	if xt != nil {
		return xt
	}
	return yt
}

func (c *checker) checkUnaryExpr(e *ast.UnaryExpr) Type {
	xt := c.checkExpr(e.X)
	switch e.Op {
	case token.AND:
		// Address-of: &x → *T
		if xt != nil {
			return &Pointer{base: xt}
		}
		return Typ[Invalid]
	case token.MUL:
		// Dereference: *x → T (handled as StarExpr, but unary MUL can also occur)
		if pt, ok := xt.(*Pointer); ok {
			return pt.base
		}
		return xt
	case token.NOT:
		return Typ[Bool]
	case token.ARROW:
		c.errorf(e.Pos(), "channel receive is not permitted in Plutonium")
		return Typ[Invalid]
	case token.SUB, token.XOR:
		// Negation and bitwise complement return same type
		return xt
	case token.ADD:
		return xt
	}
	return xt
}

func (c *checker) checkCallExpr(e *ast.CallExpr) Type {
	// Check for forbidden built-ins by name before resolving
	if ident, ok := e.Fun.(*ast.Ident); ok {
		switch ident.Name {
		case "new":
			c.errorf(e.Pos(), "'new' is forbidden in Plutonium (no implicit heap allocation)")
			// still check args to catch nested errors
			for _, arg := range e.Args {
				c.checkExpr(arg)
			}
			return Typ[Invalid]

		case "make":
			// make is forbidden for slices and channels/maps.
			// make([]T, n) — first arg is an *ast.ArrayType with Len==nil
			c.errorf(e.Pos(), "'make' is forbidden in Plutonium (no implicit heap allocation)")
			for _, arg := range e.Args {
				c.checkExpr(arg)
			}
			return Typ[Invalid]

		case "panic":
			c.errorf(e.Pos(), "'panic' is forbidden in Plutonium (no runtime unwinder)")
			for _, arg := range e.Args {
				c.checkExpr(arg)
			}
			return Typ[Invalid]

		case "recover":
			c.errorf(e.Pos(), "'recover' is forbidden in Plutonium (no runtime unwinder)")
			return Typ[Invalid]

		case "delete":
			c.errorf(e.Pos(), "'delete' is forbidden in Plutonium (no built-in maps)")
			for _, arg := range e.Args {
				c.checkExpr(arg)
			}
			return Typ[Invalid]

		case "close":
			c.errorf(e.Pos(), "'close' is forbidden in Plutonium (no channels)")
			for _, arg := range e.Args {
				c.checkExpr(arg)
			}
			return Typ[Invalid]
		}
	}

	funType := c.checkExpr(e.Fun)
	for _, arg := range e.Args {
		c.checkExpr(arg)
	}

	// Type conversion: int32(x), uint64(p), etc.
	if bt, ok := funType.(*Basic); ok {
		_ = bt
		return funType
	}

	// Named type conversion: MyType(x)
	if named, ok := funType.(*Named); ok {
		_ = named
		return funType
	}

	// Function call via signature
	if sig, ok := funType.(*Signature); ok {
		if len(sig.results) == 1 {
			return sig.results[0].typ
		}
		if len(sig.results) == 0 {
			return nil
		}
		// Multiple return values — return first result for now
		return sig.results[0].typ
	}

	// Built-in function calls (non-forbidden)
	if _, ok := funType.(*Builtin); ok {
		// len, cap return int; copy returns int; append returns slice
		// For now, return invalid (caller doesn't usually use the result type)
		return Typ[Int]
	}

	return Typ[Invalid]
}

func (c *checker) checkSelectorExpr(e *ast.SelectorExpr) Type {
	// Handle unsafe package selectors before general expression checking
	if ident, ok := e.X.(*ast.Ident); ok && ident.Name == "unsafe" {
		// Verify "unsafe" is in scope (i.e., was imported)
		obj := c.currentScope().Lookup("unsafe")
		if obj != nil && obj.Kind == PkgObj {
			c.info.Uses[ident] = obj
			switch e.Sel.Name {
			case "Pointer":
				return Typ[UnsafePointer]
			case "Sizeof", "Alignof", "Offsetof":
				// These are pseudo-builtin functions; their call returns uintptr.
				// Return uintptr here — the call site in checkCallExpr handles
				// Basic types as type conversions, which works for uintptr.
				return Typ[Uintptr]
			}
		}
	}

	// Check if X is a package identifier for an imported package
	if ident, ok := e.X.(*ast.Ident); ok {
		if pkg, ok := c.imports[ident.Name]; ok {
			// Mark the package ident as used
			pkgObj := c.currentScope().Lookup(ident.Name)
			if pkgObj != nil {
				c.info.Uses[ident] = pkgObj
			}
			// Look up the selector in the imported package's scope
			obj := pkg.Scope().LookupLocal(e.Sel.Name)
			if obj == nil {
				c.errorf(e.Sel.Pos(), "%s.%s not found in package", ident.Name, e.Sel.Name)
				return Typ[Invalid]
			}
			c.info.Uses[e.Sel] = obj
			return obj.Type
		}
	}

	xt := c.checkExpr(e.X)
	if xt == nil {
		return Typ[Invalid]
	}

	// Dereference pointer
	underlying := xt
	if pt, ok := xt.(*Pointer); ok {
		underlying = pt.base
	}

	// Look up field in named struct
	if named, ok := underlying.(*Named); ok {
		underlying = named.underlying
		// Check methods first
		for _, m := range named.methods {
			if m.name == e.Sel.Name {
				return m.sig
			}
		}
	}

	// Look up field in struct
	if st, ok := underlying.(*Struct); ok {
		for _, f := range st.fields {
			if f.name == e.Sel.Name {
				return f.typ
			}
		}
	}

	return Typ[Invalid]
}

func (c *checker) checkIndexExpr(e *ast.IndexExpr) Type {
	xt := c.checkExpr(e.X)
	c.checkExpr(e.Index)

	if xt == nil {
		return Typ[Invalid]
	}

	// Unwrap named types
	underlying := xt
	if named, ok := xt.(*Named); ok {
		underlying = named.underlying
	}

	switch t := underlying.(type) {
	case *Slice:
		return t.elem
	case *Array:
		return t.elem
	case *Pointer:
		if arr, ok := t.base.(*Array); ok {
			return arr.elem
		}
	}
	return Typ[Invalid]
}

func (c *checker) checkCompositeLit(e *ast.CompositeLit) Type {
	var litType Type
	if e.Type != nil {
		litType = c.resolveType(e.Type)
	}
	for _, elt := range e.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			// In a struct composite literal, the key is a field name identifier.
			// Don't look it up in scope — it refers to a struct field.
			// Only type-check the value.
			c.checkExpr(kv.Value)
		} else {
			c.checkExpr(elt)
		}
	}
	return litType
}
