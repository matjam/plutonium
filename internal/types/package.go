package types

import (
	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/token"
)

// Package represents a type-checked Plutonium package.
type Package struct {
	path  string
	name  string
	scope *Scope
	files []*ast.File
}

// Path returns the package import path.
func (p *Package) Path() string { return p.path }

// Name returns the package name.
func (p *Package) Name() string { return p.name }

// Scope returns the package scope.
func (p *Package) Scope() *Scope { return p.scope }

// Files returns the AST files belonging to this package.
func (p *Package) Files() []*ast.File { return p.files }

// Scope is a declaration scope with typed objects.
type Scope struct {
	parent  *Scope
	objects map[string]*Object
}

// NewScope creates a new scope nested in the parent scope.
func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, objects: make(map[string]*Object)}
}

// Lookup returns the object with the given name, searching parent scopes.
func (s *Scope) Lookup(name string) *Object {
	if obj := s.objects[name]; obj != nil {
		return obj
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return nil
}

// LookupLocal returns the object in this scope only (no parent search).
func (s *Scope) LookupLocal(name string) *Object {
	return s.objects[name]
}

// Insert attempts to insert obj into the scope.
// If a name collision exists, returns the existing object without inserting.
// Returns nil on success.
func (s *Scope) Insert(obj *Object) *Object {
	if existing := s.objects[obj.Name]; existing != nil {
		return existing
	}
	s.objects[obj.Name] = obj
	return nil
}

// ObjKind identifies what kind of entity an Object represents.
type ObjKind int

const (
	BadObj     ObjKind = iota
	PkgObj             // package
	ConObj             // constant
	TypObj             // type name
	VarObj             // variable (including function parameters)
	FunObj             // function or method
	LblObj             // label
	BuiltinObj         // built-in function
	NilObj             // nil
)

// Object represents a named entity in a scope.
type Object struct {
	Kind  ObjKind
	Name  string
	Type  Type        // resolved type (nil for packages, labels)
	Value interface{} // for constants: the constant value
	Pos   token.Pos
}

// Func is a function or method object.
type Func struct {
	name string
	sig  *Signature
}

func (f *Func) Name() string          { return f.name }
func (f *Func) Signature() *Signature { return f.sig }

// TypeName is a named type object.
type TypeName struct {
	name string
	typ  *Named
}

func (t *TypeName) Name() string { return t.name }

// Var is a variable or parameter.
type Var struct {
	name string
	typ  Type
}

func (v *Var) Name() string { return v.name }
func (v *Var) Type() Type   { return v.typ }
