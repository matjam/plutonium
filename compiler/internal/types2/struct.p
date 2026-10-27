// Translated by go2pu from internal/types2/struct.go at the go-cutover tag; edited by hand since.

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types2

import (
	"pluto/strconv"
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	. "compiler/internal/types2/errors"
	"compiler/pu"
)

// ----------------------------------------------------------------------------
// API

// A Struct represents a struct type.
type Struct struct {
	fields []*Var   // fields != nil indicates the struct is set up (possibly with len(fields) == 0)
	tags   []string // field tags; nil if there are no tags
}

// NewStruct returns a new struct with the given fields and corresponding field tags.
// If a field with index i has a tag, tags[i] must be that tag, but len(tags) may be
// only as long as required to hold the tag with the largest index i. Consequently,
// if no field has a tag, tags may be nil.
func NewStruct(fields []*Var, tags []string) *Struct {
	var fset objset
	for _, f := range fields {
		if f.name != "_" && fset.insert(f) != nil {
			panic("multiple fields with the same name")
		}
	}
	if len(tags) > len(fields) {
		panic("more tags than fields")
	}
	s := pu.New(Struct{fields: fields, tags: tags})
	s.markComplete()
	return s
}

// NumFields returns the number of fields in the struct (including blank and embedded fields).
func (s *Struct) NumFields() int { return len(s.fields) }

// Field returns the i'th field for 0 <= i < NumFields().
func (s *Struct) Field(i int) *Var { return s.fields[i] }

// Tag returns the i'th field tag for 0 <= i < NumFields().
func (s *Struct) Tag(i int) string {
	if i < len(s.tags) {
		return s.tags[i]
	}
	return ""
}

func (t *Struct) Underlying() Type { return t }
func (t *Struct) String() string   { return TypeString(t, nil) }

// ----------------------------------------------------------------------------
// Implementation

func (s *Struct) markComplete() {
	if s.fields == nil {
		s.fields = compat.Make[*Var](0)
	}
}

func (check *Checker) structType(styp *Struct, e *ast.StructType) {
	list := e.Fields
	if list == nil {
		styp.markComplete()
		return
	}
	check.later(pu.New(goClosure_structType_1{check: check, styp: styp}))

	// struct fields and tags
	var fields []*Var
	var tags []string

	// for double-declaration checks
	var fset objset

	// current field typ and tag
	var typ Type
	var tag string
	add := func(ident *ast.Ident, embedded bool) {
		if tag != "" && tags == nil {
			tags = compat.Make[string](len(fields))
		}
		if tags != nil {
			tags = compat.Append(tags, tag)
		}

		pos := ident.Pos()
		name := ident.Name
		fld := NewField(pos, check.pkg, name, typ, embedded)
		// spec: "Within a struct, non-blank field names must be unique."
		if name == "_" || check.declareInSet(&fset, pos, fld) {
			fields = compat.Append(fields, fld)
			check.recordDef(ident, fld)
		}
	}

	// addInvalid adds an embedded field of invalid type to the struct for
	// fields with errors; this keeps the number of struct fields in sync
	// with the source as long as the fields are _ or have different names
	// (go.dev/issue/25627).
	addInvalid := func(ident *ast.Ident) {
		typ = Typ[Invalid]
		tag = ""
		add(ident, true)
	}

	for _, f := range list.List {
		typ = check.varType(f.Type)
		tag = check.tag(f.Tag)
		if len(f.Names) > 0 {
			// named fields
			for _, name := range f.Names {
				add(name, false)
			}
		} else {
			// embedded field
			// spec: "An embedded type must be specified as a type name T or as a
			// pointer to a non-interface type name *T, and T itself may not be a
			// pointer type."
			pos := f.Type.Pos() // position of type, for errors
			name := embeddedFieldIdent(f.Type)
			if w, ok := Unalias(typ).(*Wrapper); ok && w.IsPointer() {
				// owned[T], shared[T] and synced[T] embed like *T: the
				// field is named T (language spec section 7.19).
				if ix, ok := f.Type.(*ast.IndexExpr); ok {
					name = embeddedFieldIdent(ix.Index)
				}
			}
			if name == nil {
				check.errorf(f.Type, InvalidSyntaxTree, "embedded field type %s has no name", f.Type)
				name = ast.NewIdent("_")
				name.NamePos = pos
				addInvalid(name)
				continue
			}
			add(name, true) // struct{p.T} field has position of T

			// Because we have a name, typ must be of the form T or *T, where T is the name
			// of a (named or alias) type, and t (= deref(typ)) must be the type of T.
			// We must delay this check to the end because we don't want to instantiate
			// (via t.Underlying()) a possibly incomplete type.

			// for use in the closure below
			embeddedTyp := typ
			embeddedPos := f.Type

			check.later(pu.New(goClosure_structType_2{embeddedTyp: embeddedTyp, check: check, embeddedPos: embeddedPos})).describef(embeddedPos, "check embedded type %s", embeddedTyp)
		}
	}

	styp.fields = fields
	styp.tags = tags
	styp.markComplete()
}

func embeddedFieldIdent(e ast.Expr) *ast.Ident {
	switch e := e.(type) {
	case *ast.Ident:
		return e
	case *ast.StarExpr:
		// *T is valid, but **T is not
		if _, ok := e.X.(*ast.StarExpr); !ok {
			return embeddedFieldIdent(e.X)
		}
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.IndexExpr:
		return embeddedFieldIdent(e.X)
	case *ast.IndexListExpr:
		return embeddedFieldIdent(e.X)
	}
	return nil // invalid embedded field
}

func (check *Checker) declareInSet(oset *objset, pos token.Pos, obj Object) bool {
	if alt := oset.insert(obj); alt != nil {
		err := check.newError(DuplicateDecl)
		err.addf(atPos(pos), "%s redeclared", pu.Box(obj.Name()))
		err.addAltDecl(alt)
		err.report()
		return false
	}
	return true
}

func (check *Checker) tag(t *ast.BasicLit) string {
	if t != nil {
		if t.Kind == token.STRING {
			if val, err := strconv.Unquote(t.Value); err == nil {
				return val
			}
		}
		check.errorf(t, InvalidSyntaxTree, "incorrect tag syntax: %q", pu.Box(t.Value))
	}
	return ""
}

// goClosure_structType_1 is the function literal at struct.go:74, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_structType_1 struct {
	check *Checker
	styp  *Struct
}

func (goSelf *goClosure_structType_1) Call() { goSelf.check.checkUnnamedStruct(goSelf.styp) }

// goClosure_structType_2 is the function literal at struct.go:154, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_structType_2 struct {
	embeddedTyp Type
	check       *Checker
	embeddedPos ast.Expr
}

func (goSelf *goClosure_structType_2) Call() {
	t, isPtr := derefBox(goSelf.embeddedTyp)
	switch u := t.Underlying().(type) {
	case *Basic:
		if !isValid(t) {
			// error was reported before
			return
		}
		// unsafe.Pointer is treated like a regular pointer
		if u.kind == UnsafePointer {
			goSelf.check.error(goSelf.embeddedPos, InvalidPtrEmbed, "embedded field type cannot be unsafe.Pointer")
		}
	case *Pointer:
		goSelf.check.error(goSelf.embeddedPos, InvalidPtrEmbed, "embedded field type cannot be a pointer")
	case *Interface:
		if isTypeParam(t) {
			// The error code here is inconsistent with other error codes for
			// invalid embedding, because this restriction may be relaxed in the
			// future, and so it did not warrant a new error code.
			goSelf.check.error(goSelf.embeddedPos, MisplacedTypeParam, "embedded field type cannot be a (pointer to a) type parameter")
			break
		}
		if isPtr {
			goSelf.check.error(goSelf.embeddedPos, InvalidPtrEmbed, "embedded field type cannot be a pointer to an interface")
		}
	}
}
