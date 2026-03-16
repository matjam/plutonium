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
		// Check imported packages for qualified type names (e.g. pkg.Type)
		if xIdent, ok := e.X.(*ast.Ident); ok {
			if pkg, ok := c.imports[xIdent.Name]; ok {
				obj := pkg.Scope().LookupLocal(e.Sel.Name)
				if obj != nil && obj.Kind == TypObj {
					return obj.Type
				}
			}
		}
		return Typ[Invalid]

	case *ast.IndexExpr:
		// Generic instantiation like Stack[int32] — resolve base type for now
		return c.resolveType(e.X)

	case *ast.ParenExpr:
		return c.resolveType(e.X)

	case *ast.Ellipsis:
		// ...T → resolve element type (used in variadic param context)
		return c.resolveType(e.Elt)

	case *ast.PackedType:
		return c.resolvePackedType(e)

	case *ast.BitsType:
		// BitsType in isolation — resolve just the access type
		return c.resolveType(e.AccessType)

	case *ast.BitsGroupType:
		// BitsGroupType in isolation — a group of 1-bit bools
		return Typ[Bool]

	case *ast.VolatileType:
		elem := c.resolveType(e.Elem)
		// volatile[T] — T must be a scalar type (reject structs, arrays, slices)
		if elem != nil {
			switch elem.(type) {
			case *Struct, *Array, *Slice:
				c.errorf(e.Volatile, "volatile[T] requires a scalar type; %s is not scalar", elem.String())
				return Typ[Invalid]
			}
		}
		return &Volatile{elem: elem}

	case *ast.AlignedType:
		elem := c.resolveType(e.Elem)
		// Parse the alignment constant
		alignment := 0
		if lit, ok := e.Alignment.(*ast.BasicLit); ok {
			fmt.Sscanf(lit.Value, "%d", &alignment)
		}
		if alignment <= 0 {
			c.errorf(e.Alignment.Pos(), "alignment must be a positive integer")
			return Typ[Invalid]
		}
		if alignment&(alignment-1) != 0 {
			c.errorf(e.Alignment.Pos(), "alignment %d must be a power of 2", alignment)
			return Typ[Invalid]
		}
		return &Aligned{elem: elem, alignment: alignment}
	}

	return Typ[Invalid]
}

func (c *checker) resolveIdentType(e *ast.Ident) Type {
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
	var variadic bool
	if e.Params != nil {
		for _, field := range e.Params.List {
			// Check if this is a variadic parameter (...T)
			if ell, ok := field.Type.(*ast.Ellipsis); ok {
				variadic = true
				// The variadic element type becomes a slice param
				elemType := c.resolveType(ell.Elt)
				sliceType := &Slice{elem: elemType}
				if len(field.Names) == 0 {
					params = append(params, &Var{name: "_", typ: sliceType})
				} else {
					for _, name := range field.Names {
						params = append(params, &Var{name: name.Name, typ: sliceType})
					}
				}
				continue
			}
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
	return &Signature{params: params, results: results, variadic: variadic}
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

func (c *checker) resolvePackedType(e *ast.PackedType) *Packed {
	backing := c.resolveType(e.BackingType)

	// Map AST endianness to types endianness
	var endian Endianness
	switch e.ByteOrder {
	case ast.EndianBig:
		endian = EndianBig
	case ast.EndianLittle:
		endian = EndianLittle
	default:
		endian = EndianNative
	}

	// Determine the backing type's bit width
	backingBits := 0
	if bt, ok := backing.(*Basic); ok {
		switch bt.kind {
		case Uint8, Int8, Byte:
			backingBits = 8
		case Uint16, Int16:
			backingBits = 16
		case Uint32, Int32, Rune:
			backingBits = 32
		case Uint64, Int64, Int, Uint, Uintptr:
			backingBits = 64
		}
	}

	var fields []*BitField
	bitOffset := 0
	if e.Fields != nil {
		for _, field := range e.Fields.List {
			switch ft := field.Type.(type) {
			case *ast.BitsType:
				accessType := c.resolveType(ft.AccessType)

				// Parse the bit width from the literal
				width := 0
				if lit, ok := ft.Width.(*ast.BasicLit); ok {
					fmt.Sscanf(lit.Value, "%d", &width)
				}

				if width <= 0 {
					c.errorf(ft.Width.Pos(), "bit width must be a positive integer")
					continue
				}

				for _, name := range field.Names {
					fields = append(fields, &BitField{
						name:       name.Name,
						accessType: accessType,
						width:      width,
						offset:     bitOffset,
					})
					bitOffset += width
				}
			case *ast.BitsGroupType:
				// bits { FLAG_A, FLAG_B, ... } — each sub-field is 1-bit bool
				groupName := ""
				if len(field.Names) > 0 {
					groupName = field.Names[0].Name
				}
				for _, subName := range ft.Names {
					fields = append(fields, &BitField{
						name:       subName.Name,
						accessType: Typ[Bool],
						width:      1,
						offset:     bitOffset,
						groupName:  groupName,
					})
					bitOffset++
				}
			default:
				c.errorf(field.Pos(), "packed type fields must use bits[T, N] or bits { ... } type")
			}
		}
	}

	if backingBits > 0 && bitOffset > backingBits {
		c.errorf(e.Packed, "packed type fields use %d bits, exceeds backing type's %d bits", bitOffset, backingBits)
	}

	return &Packed{
		backing:    backing,
		fields:     fields,
		totalBits:  bitOffset,
		endianness: endian,
	}
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
