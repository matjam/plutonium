package types2

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// Owners (language spec section 6.4).
//
// A type is an owner only if its definition says so: type T owner .... The
// checker records the mark on the TypeName, so that every instance of a
// generic owner shares it, and enforces the rules about where the mark may
// and must appear. Moves and drops are the business of the move checker
// (internal/owner) and lowering.

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
		return t.Origin().obj.ownerMark
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

const ownerSpec = `(see "Ownership" in the language spec, section 6.4)`

// ownerDecl records the owner mark of a type declaration and schedules the
// checks that need the declared type.
func (check *Checker) ownerDecl(obj *TypeName, tdecl *ast.TypeSpec) {
	if !tdecl.Owner.IsValid() {
		check.later(func() { check.checkNotOwner(obj, tdecl) })
		return
	}
	if tdecl.Assign.IsValid() {
		check.errorf(atPos(tdecl.Owner), UnsupportedFeature,
			"owner can only be used in a type definition: type %s owner ...\n"+
				"    An alias (type %s = ...) is another name for an existing type and has\n"+
				"    that type's kind, owner or not; it cannot add a mark of its own.\n"+
				"    %s", obj.name, obj.name, ownerSpec)
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
	// what names the kind; why continues "owner marks a type whose values
	// own something and move instead of being copied." with the reason,
	// wrapped to the message's width.
	var what, why string
	if EnumOf(named) != nil {
		what = "an enum type"
		why = "An enum is a set of integer constants, and its values are always\n    copied."
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
				why = "A string is a view: it looks at bytes that\n    something else owns, and never frees them."
			case u.info&IsBoolean != 0:
				what = "a boolean type"
				why = "A boolean is a value, and its values are always\n    copied."
			default:
				what = "a " + u.name + " type"
				why = "A number is a value, and its values are always\n    copied."
			}
		case *Slice:
			what = "a slice type"
			why = "A slice is a view: it looks at an array that\n    something else owns, and never frees it."
		case *Map:
			what = "a map type"
			why = "Plutonium has no built-in maps; use hash.Map, which is\n    an owner."
		case *Chan:
			what = "a channel type"
			why = "A channel is counted: its copies share it, and the\n    last one frees it."
		case *Signature:
			what = "a function type"
			why = "A function value is a value, or a view of the frame\n    whose variables it captures."
		case *Packed:
			what = "a packed type"
			why = "A packed type is bits, and its values are always\n    copied."
		case *Wrapper:
			what = "a " + u.kind.String() + " type"
			why = u.kind.String() + "[T] already has a kind of its own (section 5.6)."
		default:
			return
		}
	}
	check.errorf(atPos(tdecl.Owner), UnsupportedFeature,
		"owner cannot qualify %s\n"+
			"    owner marks a type whose values own something and move instead of being\n"+
			"    copied. %s To attach a resource to a %s, wrap both in an owner:\n"+
			"        type Handle owner struct {\n"+
			"            v  %s\n"+
			"            fd int32\n"+
			"        }\n"+
			"    %s", what, why, obj.name, obj.name, ownerSpec)
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
			check.errorf(tdecl.Type, UnsupportedFeature,
				"%s must be declared owner: %s is defined from %s, which is an owner\n"+
					"    An owner owns memory and frees it when dropped, so it moves instead of\n"+
					"    being copied. A type defined from it owns that memory too.\n"+
					"    Plutonium does not make %s an owner silently; say so in its declaration:\n"+
					"        type %s owner %s\n"+
					"    %s", obj.name, obj.name, check.sprintf("%s", r), obj.name, obj.name,
				check.sprintf("%s", r), ownerSpec)
		}
		return
	case *Struct:
		check.ownerFields(obj.name, r, tdecl)
	case *Array:
		if IsOwner(r.elem) {
			// [N]T of owners is an owner by its element, but a named
			// type must still say so.
			check.errorf(tdecl.Type, UnsupportedFeature,
				"%s must be declared owner: it is %s, an array of owners\n"+
					"    An owner owns memory and frees it when dropped, so it moves instead of\n"+
					"    being copied. An array of owners owns their memory too.\n"+
					"    Plutonium does not make %s an owner silently; say so in its declaration:\n"+
					"        type %s owner %s\n"+
					"    %s", obj.name, check.sprintf("%s", r), obj.name, obj.name, check.sprintf("%s", r), ownerSpec)
		}
	case *Interface:
		for _, e := range r.embeddeds {
			if n, ok := Unalias(e).(*Named); ok && n.Origin().obj.ownerMark {
				check.errorf(tdecl.Type, UnsupportedFeature,
					"%s must be declared owner: it embeds %s, an owning interface\n"+
						"    An owning interface owns the value it holds and drops it. An interface\n"+
						"    that embeds one holds its values the same way.\n"+
						"        type %s owner interface {\n"+
						"            ...\n"+
						"        }\n"+
						"    %s", obj.name, check.sprintf("%s", n), obj.name, ownerSpec)
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
		decl := "type " + name + " owner struct {"
		if name == "" {
			check.errorf(at, UnsupportedFeature,
				"a struct type without a name cannot have owner fields: %s is a %s, %s\n"+
					"    An owner owns memory and frees it when dropped, so it moves instead of\n"+
					"    being copied. A struct that contains one owns that memory too, and only\n"+
					"    a named type can be declared an owner. Declare one:\n"+
					"        type T owner struct {\n"+
					"            ...\n"+
					"        }\n"+
					"    %s", what, check.sprintf("%s", f.typ), kind, ownerSpec)
			return
		}
		check.errorf(at, UnsupportedFeature,
			"%s must be declared owner: %s is a %s, %s\n"+
				"    An owner owns memory and frees it when dropped, so it moves instead of\n"+
				"    being copied. A struct that contains one owns that memory too, and copying\n"+
				"    the struct would copy the owner, leaving two values that both free it.\n"+
				"    Plutonium does not make %s an owner silently; say so in its declaration:\n"+
				"        %s\n"+
				"            ...\n"+
				"        }\n"+
				"    %s values then move, and %s is dropped when a %s is.\n"+
				"    If %s should only look at a value that something else owns, store a pointer:\n"+
				"        %s *%s\n"+
				"    %s", name, what, check.sprintf("%s", f.typ), kind, name, decl, name, f.name, name,
			name, f.name, check.sprintf("%s", f.typ), ownerSpec)
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
				check.errorf(m, UnsupportedFeature,
					"Drop must have no parameters and no results\n"+
						"    Drop runs automatically when a value is destroyed. The compiler calls it\n"+
						"    with nothing and ignores any result.\n"+
						"    If this method is not meant as a destructor, give it another name.\n"+
						"    %s", ownerSpec)
				continue
			}
			if !tn.ownerMark {
				kw := "struct"
				switch named.Underlying().(type) {
				case *Interface:
					kw = "interface"
				case *Array:
					kw = check.sprintf("%s", named.Underlying())
				case *Basic, *Pointer:
					kw = check.sprintf("%s", named.Underlying())
				}
				body := " {\n            ...\n        }"
				if kw != "struct" && kw != "interface" {
					body = ""
				}
				check.errorf(m, UnsupportedFeature,
					"%s has a Drop method but is not declared owner\n"+
						"    Drop runs automatically when a value is destroyed. That only makes sense\n"+
						"    for a type whose values have exactly one owner: if %s values were copied\n"+
						"    as ordinary values are, every copy would run Drop and release the same\n"+
						"    resource.\n"+
						"    Declare the type as an owner, so that its values move instead of being copied:\n"+
						"        type %s owner %s%s\n"+
						"    %s", tn.name, tn.name, tn.name, kw, body, ownerSpec)
			}
		}
	}
}

// ownerTypeArg reports an owner type argument for a type parameter that
// does not admit owners, or "".
func (check *Checker) ownerTypeArg(tpar *TypeParam, targ Type) string {
	if tpar.obj.ownerMark || !IsOwner(targ) {
		return ""
	}
	if _, ok := Unalias(targ).(*TypeParam); ok {
		// An owner type parameter passed on: the same rule, worded for it.
		return fmt.Sprintf("%s allows owners and %s does not", targ, tpar.obj.name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s is an owner and %s does not allow owners\n", check.sprintf("%s", targ), tpar.obj.name)
	b.WriteString("    Inside the generic code, values of " + tpar.obj.name + " are copied freely. Copying\n")
	b.WriteString("    an owner would leave several values that all free the same memory.\n")
	b.WriteString("    If the generic code moves each value of " + tpar.obj.name + " at most once, allow owners\n")
	b.WriteString("    in its declaration:\n")
	b.WriteString("        [" + tpar.obj.name + " owner any]\n")
	b.WriteString("    Otherwise use explicit copies made with Clone.\n")
	b.WriteString("    " + ownerSpec)
	return b.String()
}
