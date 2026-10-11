package types2

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// Owners (language spec section 7.18).
//
// A type is an owner only if its definition says so: type T owner .... The
// checker records the mark on the TypeName, so that every instance of a
// generic owner shares it, and enforces the rules about where the mark may
// and must appear. Moves and drops are the business of the move checker
// (internal/moves) and lowering.

// IsOwner reports whether values of T are owners: they move on assignment
// and are dropped at the end of their scope.
func IsOwner(T Type) bool {
	return isOwner(T, map[Type]bool{})
}

func isOwner(T Type, seen map[Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	switch t := Unalias(T).(type) {
	case *Named:
		if t.Origin().obj.ownerMark {
			return true
		}
		// A type defined from owned[T] owns its object like owned[T].
		w, ok := t.Underlying().(*Wrapper)
		return ok && w.kind == WrapOwned
	case *TypeParam:
		return t.obj.ownerMark
	case *Array:
		return isOwner(t.elem, seen)
	case *Wrapper:
		switch t.kind {
		case WrapOwned:
			return true
		case WrapRing, WrapAligned:
			return isOwner(t.elem, seen)
		}
	}
	return false
}

// IsOwnerTypeParam reports whether type parameter tp admits owners
// ([T owner any]).
func IsOwnerTypeParam(tp *TypeParam) bool { return tp.obj.ownerMark }

// An Explanation is a Plutonium error's explanation specialised to the code
// at fault: what happened and the idea behind the rule, with the names of
// the program, and fixes as code. internal/diag prints it under the
// message, followed by the rule's spec section.
type Explanation struct {
	Text  string // paragraphs separated by "\n"
	Fixes []ExplainFix
}

// An ExplainFix is a fix: a sentence introducing it and the code to write
// (none for advice without code).
type ExplainFix struct {
	Intro, Code string
}

// explainf reports an error with an explanation of its own.
func (check *Checker) explainf(at positioner, code Code, ex *Explanation, format string, args ...any) {
	err := check.newError(code)
	err.addf(at, format, args...)
	err.explain = ex
	err.report()
}

// whatOwners is the idea every ownership explanation starts from.
const whatOwners = "An owner owns memory or another resource and releases it when it is dropped, so it moves instead of being copied."

// ownerDecl records the owner mark of a type declaration and schedules the
// checks that need the declared type.
func (check *Checker) ownerDecl(obj *TypeName, tdecl *ast.TypeSpec) {
	if !tdecl.Owner.IsValid() {
		check.later(func() { check.checkNotOwner(obj, tdecl) })
		return
	}
	if tdecl.Assign.IsValid() {
		check.explainf(atPos(tdecl.Owner), OwnerAlias, &Explanation{
			Text: "An alias (type " + obj.name + " = ...) is another name for an existing type and has that type's kind, owner or not. It cannot add a mark of its own.",
			Fixes: []ExplainFix{{
				Intro: "To make a new owner type, define one",
				Code:  "type " + obj.name + " owner ...",
			}},
		}, "owner can only be used in a type definition: type %s owner ...", obj.name)
		return
	}
	obj.ownerMark = true
	check.later(func() { check.checkOwnerKind(obj, tdecl) })
}

// checkOwnerKind reports an owner mark on a kind of type that cannot own.
func (check *Checker) checkOwnerKind(obj *TypeName, tdecl *ast.TypeSpec) {
	named := asNamed(obj.typ)
	if named == nil {
		return
	}
	// what names the kind; why says why it cannot own.
	var what, why string
	if EnumOf(named) != nil {
		what = "an enum type"
		why = "An enum is a set of integer constants, and its values are always copied."
	} else {
		switch u := named.fromRHS.Underlying().(type) {
		case *Struct, *Array, *Pointer, *Interface:
			return
		case *Basic:
			switch {
			case u.info&IsInteger != 0, u.kind == UnsafePointer:
				return
			case u.info&IsString != 0:
				what = "a string type"
				why = "A string is a value: its bytes never change and count their own references, so copying it is always safe."
			case u.info&IsBoolean != 0:
				what = "a boolean type"
				why = "A boolean is a value, and its values are always copied."
			default:
				what = "a " + u.name + " type"
				why = "A number is a value, and its values are always copied."
			}
		case *Slice:
			what = "a slice type"
			why = "A slice is a view: it looks at an array that something else owns, and never frees it."
		case *Map:
			what = "a map type"
			why = "Plutonium has no built-in maps; use hash.Map, which is an owner."
		case *Chan:
			what = "a channel type"
			why = "A channel is counted: its copies share it, and the last one frees it."
		case *Signature:
			what = "a function type"
			why = "A function value is a value, or a view of the frame whose variables it captures."
		case *Packed:
			what = "a packed type"
			why = "A packed type is bits, and its values are always copied."
		case *Wrapper:
			what = "a " + u.kind.String() + " type"
			why = u.kind.String() + "[T] already has a kind of its own (section 5.6)."
		default:
			return
		}
	}
	check.explainf(atPos(tdecl.Owner), BadOwnerKind, &Explanation{
		Text: "owner marks a type whose values own something and move instead of being copied. " + why,
		Fixes: []ExplainFix{{
			Intro: "To attach a resource to " + article(obj.name) + ", wrap both in an owner",
			Code:  "type Handle owner struct {\n\tv  " + obj.name + "\n\tfd int32\n}",
		}},
	}, "owner cannot qualify %s", what)
}

// article puts "a" or "an" before a name.
func article(s string) string {
	if s != "" && (s[0] == 'A' || s[0] == 'E' || s[0] == 'I' || s[0] == 'O' || s[0] == 'U' ||
		s[0] == 'a' || s[0] == 'e' || s[0] == 'i' || s[0] == 'o' || s[0] == 'u') {
		return "an " + s
	}
	return "a " + s
}

// mustBeOwner reports a type definition that holds owners without being
// declared owner. what says what holds one ("its field buf is a Buf, which
// is an owner"), part how the type's values own it, decl the declaration to
// write.
func (check *Checker) mustBeOwner(at positioner, name, what, part, decl string, view *ExplainFix) {
	ex := &Explanation{
		Text: whatOwners + " " + part,
		Fixes: []ExplainFix{{
			Intro: "Plutonium does not make " + name + " an owner silently; say so in its declaration",
			Code:  decl,
		}},
	}
	if view != nil {
		ex.Fixes = append(ex.Fixes, *view)
	}
	check.explainf(at, MustBeOwner, ex, "%s must be declared owner: %s", name, what)
}

// checkNotOwner reports a type definition without the owner mark whose
// values would own something: an owner field, an embedded owner, an array
// of owners, or a definition from an owner type.
func (check *Checker) checkNotOwner(obj *TypeName, tdecl *ast.TypeSpec) {
	named := asNamed(obj.typ)
	if named == nil || named.fromRHS == nil || !isValid(named.fromRHS) {
		return
	}
	rhs := Unalias(named.fromRHS)
	switch r := rhs.(type) {
	case *Named:
		if r.Origin().obj.ownerMark {
			from := check.sprintf("%s", r)
			check.mustBeOwner(tdecl.Type, obj.name,
				obj.name+" is defined from "+from+", which is an owner",
				"A type defined from an owner owns the same things.",
				"type "+obj.name+" owner "+from, nil)
		}
		return
	case *Struct:
		check.ownerFields(obj.name, r, tdecl)
	case *Array:
		if IsOwner(r.elem) {
			// [N]T of owners is an owner by its element, but a named
			// type must still say so.
			arr := check.sprintf("%s", r)
			check.mustBeOwner(tdecl.Type, obj.name,
				"it is "+arr+", an array of owners",
				"An array of owners owns what its elements own.",
				"type "+obj.name+" owner "+arr, nil)
		}
	case *Interface:
		for _, e := range r.embeddeds {
			if n, ok := Unalias(e).(*Named); ok && n.Origin().obj.ownerMark {
				check.mustBeOwner(tdecl.Type, obj.name,
					"it embeds "+check.sprintf("%s", n)+", an owning interface",
					"An owning interface owns the value it holds and drops it, and an interface that embeds one holds its values the same way.",
					"type "+obj.name+" owner interface {\n\t...\n}", nil)
				return
			}
		}
	}
}

// ownerFields reports the first owner field of a struct type that is not
// declared owner.
func (check *Checker) ownerFields(name string, s *Struct, tdecl *ast.TypeSpec) {
	for i, f := range s.fields {
		if !IsOwner(f.typ) {
			continue
		}
		what := "its field " + f.name
		if f.embedded {
			what = "its embedded field " + f.name
		}
		ft := check.sprintf("%s", f.typ)
		kind := "which is an owner"
		if a, ok := Unalias(f.typ).Underlying().(*Array); ok && IsOwner(a.elem) {
			if _, named := Unalias(f.typ).(*Named); !named {
				kind = "an array of owners"
			}
		}
		at := positioner(atPos(f.pos))
		if tdecl != nil {
			if st, ok := tdecl.Type.(*ast.StructType); ok {
				at = fieldNode(st, i)
			}
		}
		if name == "" {
			check.explainf(at, UnnamedOwnerStruct, &Explanation{
				Text: whatOwners + " A struct that contains one owns that memory too, and only a named type can be declared an owner.",
				Fixes: []ExplainFix{{
					Intro: "Declare a named owner type for the struct",
					Code:  "type T owner struct {\n\t...\n}",
				}},
			}, "a struct type without a name cannot have owner fields: %s is %s, %s", what, article(ft), kind)
			return
		}
		check.mustBeOwner(at, name,
			what+" is "+article(ft)+", "+kind,
			"A struct that contains one owns that memory too, and copying the struct would copy the owner, leaving two values that both free it.",
			"type "+name+" owner struct {\n\t...\n}",
			&ExplainFix{
				Intro: name + " values then move, and " + f.name + " is dropped when " + article(name) + " is. If " + name + " should only look at a value that something else owns, store a pointer",
				Code:  f.name + " *" + ft,
			})
		return
	}
}

// fieldNode returns the AST node of the i'th field of a struct type, for
// error positions.
func fieldNode(st *ast.StructType, i int) positioner {
	n := 0
	for _, f := range st.Fields.List {
		names := len(f.Names)
		if names == 0 {
			names = 1
		}
		if i < n+names {
			if len(f.Names) > 0 {
				return f.Names[i-n]
			}
			return f.Type
		}
		n += names
	}
	return atPos(st.Pos())
}

// checkUnnamedStruct reports owner fields in a struct type literal that is
// not the definition of a named type.
func (check *Checker) checkUnnamedStruct(s *Struct) {
	if check.ownerRHS[s] {
		return
	}
	check.ownerFields("", s, nil)
}

// ownerMethods checks the Drop methods of the package's types: Drop needs
// an owner type, and takes and returns nothing.
func (check *Checker) ownerMethods() {
	for _, name := range check.pkg.scope.Names() {
		tn, ok := check.pkg.scope.Lookup(name).(*TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		named := asNamed(tn.typ)
		if named == nil {
			continue
		}
		for _, m := range named.methods {
			if m.name != "Drop" {
				continue
			}
			sig, _ := m.typ.(*Signature)
			if sig != nil && (sig.params.Len() != 0 || sig.results.Len() != 0) {
				check.explainf(m, BadDropSignature, &Explanation{
					Text: "Drop runs automatically when an owner is destroyed. The compiler calls it with nothing and ignores any result.",
					Fixes: []ExplainFix{{
						Intro: "Declare it without parameters and results",
						Code:  "func (x *" + tn.name + ") Drop()",
					}, {
						Intro: "If this method is not meant to run when a value is destroyed, give it another name.",
					}},
				}, "Drop must have no parameters and no results")
				continue
			}
			if !tn.ownerMark {
				kw := "struct {\n\t...\n}"
				switch u := named.Underlying().(type) {
				case *Interface:
					kw = "interface {\n\t...\n}"
				case *Struct:
				default:
					kw = check.sprintf("%s", u)
				}
				check.explainf(m, DropNotOwner, &Explanation{
					Text: "Drop runs automatically when a value is destroyed. That only makes sense for a type whose values have exactly one owner: if " + tn.name + " values were copied as ordinary values are, every copy would run Drop and release the same resource.",
					Fixes: []ExplainFix{{
						Intro: "Declare the type as an owner, so that its values move instead of being copied",
						Code:  "type " + tn.name + " owner " + kw,
					}},
				}, "%s has a Drop method but is not declared owner", tn.name)
			}
		}
	}
}

// An ownerArgError is an owner type argument for a type parameter that
// does not admit owners.
type ownerArgError struct {
	tpar *TypeParam
	targ Type
	msg  string
	ex   *Explanation
}

func (e *ownerArgError) Error() string { return e.msg }

// ownerTypeArg returns the error for an owner type argument for a type
// parameter that does not admit owners, or nil.
func (check *Checker) ownerTypeArg(tpar *TypeParam, targ Type) *ownerArgError {
	if tpar.obj.ownerMark || !IsOwner(targ) {
		return nil
	}
	t, p := check.sprintf("%s", targ), tpar.obj.name
	e := &ownerArgError{tpar: tpar, targ: targ}
	if _, ok := Unalias(targ).(*TypeParam); ok {
		// An owner type parameter passed on: the same rule, worded for it.
		e.msg = t + " allows owners and " + p + " does not"
	} else {
		e.msg = t + " is an owner and " + p + " does not allow owners"
	}
	e.ex = &Explanation{
		Text: "Inside the generic code, values of " + p + " are copied freely. Copying an owner would leave several values that all free the same memory.",
		Fixes: []ExplainFix{{
			Intro: "If the generic code moves each value of " + p + " at most once, allow owners in its declaration",
			Code:  "[" + p + " owner any]",
		}, {
			Intro: "Otherwise use copies made explicitly with Clone, or pointers to the owners.",
		}},
	}
	return e
}

// reportTypeArg reports an error from verify for the type argument at,
// with an explanation if it is about owners. what names the generic
// function or type, for the message.
func (check *Checker) reportTypeArg(at positioner, err error, what string) {
	if oe, ok := err.(*ownerArgError); ok {
		check.explainf(at, OwnerTypeArg, oe.ex, "cannot use %s as %s in %s: %s", check.sprintf("%s", oe.targ), oe.tpar.obj.name, what, oe.msg)
		return
	}
	check.softErrorf(at, InvalidTypeArg, "%s", err)
}

// ownerConversion reports a conversion T(x) that would make a new owner
// outside the package that declares the owner type T (language spec
// section 6.4): an owner is made from a plain value only where its
// invariants are kept, as only its package can fill its unexported fields.
// Converting an owner to a non-owner reads it, and converting between
// owner types moves it; both are allowed anywhere.
func (check *Checker) ownerConversion(x *operand, T Type) {
	if !IsOwner(T) || IsOwner(x.typ()) || x.isNil() {
		return
	}
	n := asNamed(Unalias(T))
	if n == nil || n.Origin().obj.pkg == check.pkg {
		return // a box from unsafe.Pointer is unsafe code
	}
	t, v := check.sprintf("%s", T), check.sprintf("%s", x.typ())
	check.explainf(x, OwnerConversion, &Explanation{
		Text: whatOwners + " Converting a plain " + v + " into " + article(t) + " would make a second owner of whatever the value refers to (a descriptor, a page frame), and both would release it. Only the package that declares " + t + " may do that, where its invariants are kept.",
		Fixes: []ExplainFix{{
			Intro: "Use a constructor from the package that declares " + t + ".",
		}},
	}, "cannot convert %s to owner type %s outside package %s", x, t, n.Origin().obj.pkg.name)
}

// checkMapOfOwner reports a map key or element type (what) T that is an
// owner (gap G15 of the complete-spec plan, spec section 7.10): reading an
// element copies it, as in Go, and an owner cannot be copied.
func (check *Checker) checkMapOfOwner(at positioner, T Type, what string) {
	if !isValid(T) || !IsOwner(T) {
		return
	}
	t := check.sprintf("%s", T)
	ref := "synced[" + t + "]"
	if w, ok := Unalias(T).(*Wrapper); ok && w.kind == WrapOwned {
		ref = "synced[" + check.sprintf("%s", w.elem) + "]"
	}
	ex := &Explanation{
		Text: whatOwners + " A map copies its keys and elements as Go's does: v := m[k] and range give copies, and m2 := m shares the table. Two copies of an owner would both release what it owns, so a map cannot hold one.",
		Fixes: []ExplainFix{{
			Intro: "Store a counted reference to the value instead, which copies by counting and releases the value with its last copy",
			Code:  "m := map[string]" + ref + "{}",
		}},
	}
	if what == "key" {
		ex.Fixes = []ExplainFix{{Intro: "Key the map by a value that identifies the owner, such as a name or an ID, and keep the owners elsewhere."}}
	}
	check.explainf(at, MapOfOwner, ex, "map %s type %s is an owner, which a map cannot hold: reading the map copies its %ss", what, t, what)
}
