package types

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

// resolveType resolves an AST type expression to a Type.
func (c *checker) resolveType(expr ast.Expr) Type {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return c.resolveIdentType(e)

	case *ast.StarExpr:
		base := c.resolveType(e.X)
		if base == nil {
			return Typ[Invalid]
		}
		return &Pointer{base: base}

	case *ast.ArrayType:
		elem := c.resolveType(e.Elt)
		if e.Len == nil {
			// slice []T
			return &Slice{elem: elem}
		}
		// array [N]T — constant eval deferred for v0.1
		return &Array{len: 0, elem: elem}

	case *ast.StructType:
		return c.resolveStructType(e)

	case *ast.InterfaceType:
		iface := c.resolveInterfaceType(e)
		// Reject interface{} / any (empty interface)
		if iface.IsEmpty() {
			c.errorf(e.Interface, "empty interface{} is not permitted in Plutonium (no runtime type metadata)")
		}
		return iface

	case *ast.FuncType:
		return c.resolveFuncType(e)

	case *ast.PointerType:
		base := c.resolveType(e.X)
		if base == nil {
			return Typ[Invalid]
		}
		return &Pointer{base: base}

	case *ast.SelectorExpr:
		// Check for unsafe.Pointer and other unsafe pseudo-types
		if ident, ok := e.X.(*ast.Ident); ok && ident.Name == "unsafe" {
			switch e.Sel.Name {
			case "Pointer":
				return Typ[UnsafePointer]
			case "Sizeof", "Alignof", "Offsetof":
				// These are builtin functions returning uintptr; handled in infer.go
				return Typ[Uintptr]
			}
		}
		// Qualified type like pkg.Type — simplified for v0.1
		// We just return invalid without error since package resolution is limited
		return Typ[Invalid]

	case *ast.IndexExpr:
		// Generic instantiation like Stack[int32] — resolve base type for now
		return c.resolveType(e.X)

	case *ast.ParenExpr:
		return c.resolveType(e.X)
	}

	return Typ[Invalid]
}

func (c *checker) resolveIdentType(e *ast.Ident) Type {
	// Explicitly reject "any"
	if e.Name == "any" {
		c.errorf(e.Pos(), "'any' (empty interface) is not permitted in Plutonium")
		return Typ[Invalid]
	}

	obj := c.currentScope().Lookup(e.Name)
	if obj == nil {
		c.errorf(e.Pos(), "undefined type: %s", e.Name)
		return Typ[Invalid]
	}
	if obj.Kind != TypObj {
		c.errorf(e.Pos(), "%s is not a type", e.Name)
		return Typ[Invalid]
	}
	c.info.Uses[e] = obj
	return obj.Type
}

func (c *checker) resolveStructType(e *ast.StructType) *Struct {
	var fields []*StructField
	if e.Fields != nil {
		for _, field := range e.Fields.List {
			typ := c.resolveType(field.Type)
			tag := ""
			if field.Tag != nil {
				tag = field.Tag.Value
			}
			if len(field.Names) == 0 {
				// embedded field
				name := typeBaseName(typ)
				fields = append(fields, &StructField{name: name, typ: typ, tag: tag, embedded: true})
			} else {
				for _, name := range field.Names {
					fields = append(fields, &StructField{name: name.Name, typ: typ, tag: tag})
				}
			}
		}
	}
	return &Struct{fields: fields}
}

func (c *checker) resolveInterfaceType(e *ast.InterfaceType) *Interface {
	var methods []*Func
	if e.Methods != nil {
		for _, m := range e.Methods.List {
			if ft, ok := m.Type.(*ast.FuncType); ok {
				sig := c.resolveFuncType(ft)
				for _, name := range m.Names {
					methods = append(methods, &Func{name: name.Name, sig: sig})
				}
			}
		}
	}
	return &Interface{methods: methods}
}

func (c *checker) resolveFuncType(e *ast.FuncType) *Signature {
	var params []*Var
	if e.Params != nil {
		for _, field := range e.Params.List {
			typ := c.resolveType(field.Type)
			if len(field.Names) == 0 {
				params = append(params, &Var{name: "_", typ: typ})
			} else {
				for _, name := range field.Names {
					params = append(params, &Var{name: name.Name, typ: typ})
				}
			}
		}
	}
	var results []*Var
	if e.Results != nil {
		for _, field := range e.Results.List {
			typ := c.resolveType(field.Type)
			if len(field.Names) == 0 {
				results = append(results, &Var{name: "_", typ: typ})
			} else {
				for _, name := range field.Names {
					results = append(results, &Var{name: name.Name, typ: typ})
				}
			}
		}
	}
	return &Signature{params: params, results: results}
}

// resolveTypeParamConstraint resolves a type parameter constraint expression.
// Constraints may be a single type (e.g., int32) or a union (int32 | int64).
// The constraint is stored but not enforced in v0.1.
func (c *checker) resolveTypeParamConstraint(expr ast.Expr) Type {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op == token.OR {
			// Union constraint: T int32 | int64
			left := c.resolveTypeParamConstraint(e.X)
			right := c.resolveTypeParamConstraint(e.Y)
			return &Union{terms: []Type{left, right}}
		}
	case *ast.Ident:
		// Simple type name like int32
		return c.resolveIdentType(e)
	}
	return c.resolveType(expr)
}

// typeBaseName returns the base name of a type for embedded field naming.
func typeBaseName(t Type) string {
	switch t := t.(type) {
	case *Named:
		return t.obj.name
	case *Pointer:
		return typeBaseName(t.base)
	case *Basic:
		return t.name
	}
	return ""
}

// errorList collects type errors.
type errorList []typeError

type typeError struct {
	pos token.Position
	msg string
}

func (el errorList) Error() string {
	if len(el) == 0 {
		return "no errors"
	}
	if len(el) == 1 {
		return fmt.Sprintf("%s: %s", el[0].pos, el[0].msg)
	}
	return fmt.Sprintf("%s: %s (and %d more errors)", el[0].pos, el[0].msg, len(el)-1)
}

func (el *errorList) add(pos token.Position, msg string) {
	*el = append(*el, typeError{pos: pos, msg: msg})
}
