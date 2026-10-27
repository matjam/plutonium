// Translated by go2pu from internal/types2/scope.go at the go-cutover tag; edited by hand since.

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements Scopes.

package types2

import (
	"pluto/fmt"
	"pluto/io"
	"pluto/slices"
	"pluto/strings"
	"compiler/compat"
	"compiler/internal/syntax/token"
	"compiler/pu"
)

// A Scope maintains a set of objects and links to its containing
// (parent) and contained (children) scopes. Objects may be inserted
// and looked up by name. The zero value for Scope is a ready-to-use
// empty scope.
type Scope struct {
	parent   *Scope
	children []*Scope
	number   int               // parent.children[number-1] is this scope; 0 if there is no parent
	elems    map[string]Object // lazily allocated
	pos, end token.Pos         // scope extent; may be invalid
	comment  string            // for debugging only
	isFunc   bool              // set if this is a function scope (internal use only)
}

// NewScope returns a new, empty scope contained in the given parent
// scope, if any. The comment is for debugging only.
func NewScope(parent *Scope, pos, end token.Pos, comment string) *Scope {
	s := pu.New(Scope{parent, nil, 0, nil, pos, end, comment, false})
	// don't add children to Universe scope!
	if parent != nil && parent != Universe {
		parent.children = compat.Append(parent.children, s)
		s.number = len(parent.children)
	}
	return s
}

// Parent returns the scope's containing (parent) scope.
func (s *Scope) Parent() *Scope { return s.parent }

// Len returns the number of scope elements.
func (s *Scope) Len() int { return len(s.elems) }

// Names returns the scope's element names in sorted order.
func (s *Scope) Names() []string {
	names := compat.Make[string](len(s.elems))
	i := 0
	for name := range s.elems {
		names[i] = name
		i++
	}
	slices.Sort(names)
	return names
}

// NumChildren returns the number of scopes nested in s.
func (s *Scope) NumChildren() int { return len(s.children) }

// Child returns the i'th child scope for 0 <= i < NumChildren().
func (s *Scope) Child(i int) *Scope { return s.children[i] }

// Lookup returns the object in scope s with the given name if such an
// object exists; otherwise the result is nil.
func (s *Scope) Lookup(name string) Object { return s.elems[name] }

// lookupIgnoringCase returns the objects in scope s whose names match
// the given name ignoring case. If exported is set, only exported names
// are returned.
func (s *Scope) lookupIgnoringCase(name string, exported bool) []Object {
	var matches []Object
	for _, n := range s.Names() {
		if (!exported || isExported(n)) && strings.EqualFold(n, name) {
			matches = compat.Append(matches, s.Lookup(n))
		}
	}
	return matches
}

// Insert attempts to insert an object obj into scope s.
// If s already contains an alternative object alt with
// the same name, Insert leaves s unchanged and returns alt.
// Otherwise it inserts obj, sets the object's parent scope
// if not already set, and returns nil.
func (s *Scope) Insert(obj Object) Object {
	name := obj.Name()
	if alt := s.Lookup(name); alt != nil {
		return alt
	}
	s.insert(name, obj)
	// TODO(gri) Can we always set the parent to s (or is there
	// a need to keep the original parent or some race condition)?
	// If we can, than we may not need environment.lookupScope
	// which is only there so that we get the correct scope for
	// marking "used" dot-imported packages.
	if obj.Parent() == nil {
		obj.setParent(s)
	}
	return nil
}

func (s *Scope) insert(name string, obj Object) {
	if s.elems == nil {
		s.elems = make(map[string]Object)
	}
	s.elems[name] = obj
}

// WriteTo writes a string representation of the scope to w,
// with the scope elements sorted by name.
// The level of indentation is controlled by n >= 0, with
// n == 0 for no indentation.
// If recurse is set, it also writes nested (children) scopes.
func (s *Scope) WriteTo(w io.Writer, n int, recurse bool) {
	const ind = ".  "
	indn := strings.Repeat(ind, n)

	_, _ = fmt.Fprintf(w, "%s%s scope %p {\n", indn, s.comment, s)

	indn1 := compat.Concat(indn, ind)
	for _, name := range s.Names() {
		_, _ = fmt.Fprintf(w, "%s%s\n", indn1, s.Lookup(name))
	}

	if recurse {
		for _, s := range s.children {
			s.WriteTo(w, n+1, recurse)
		}
	}

	_, _ = compat.Fprintfb(w, "%s}\n", compat.ArgS(indn))
}

// String returns a string representation of the scope, for debugging.
func (s *Scope) String() string {
	buf := pu.Alloc[compat.Builder]()
	s.WriteTo(buf, 0, false)
	return buf.String()
}
