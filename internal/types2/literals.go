// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements typechecking of literals.

package types2

import (
	"github.com/matjam/plutonium/internal/constant"
	"strings"
	"unicode/utf8"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	. "github.com/matjam/plutonium/internal/types2/errors"
)

// langCompat reports an error if the representation of a numeric
// literal is not compatible with the current language version.
func (check *Checker) langCompat(lit *ast.BasicLit) {
	s := lit.Value
	if len(s) <= 2 || check.allowVersion(go1_13) {
		return
	}
	// len(s) > 2
	if strings.Contains(s, "_") {
		check.versionErrorf(lit, go1_13, "underscore in numeric literal")
		return
	}
	if s[0] != '0' {
		return
	}
	radix := s[1]
	if radix == 'b' || radix == 'B' {
		check.versionErrorf(lit, go1_13, "binary literal")
		return
	}
	if radix == 'o' || radix == 'O' {
		check.versionErrorf(lit, go1_13, "0o/0O-style octal literal")
		return
	}
	if lit.Kind != token.INT && (radix == 'x' || radix == 'X') {
		check.versionErrorf(lit, go1_13, "hexadecimal floating-point literal")
	}
}

func (check *Checker) basicLit(x *operand, e *ast.BasicLit) {
	switch e.Kind {
	case token.INT, token.FLOAT, token.IMAG:
		check.langCompat(e)
		// The max. mantissa precision for untyped numeric values
		// is 512 bits, or 4048 bits for each of the two integer
		// parts of a fraction for floating-point numbers that are
		// represented accurately in the go/constant package.
		// Constant literals that are longer than this many bits
		// are not meaningful; and excessively long constants may
		// consume a lot of space and time for a useless conversion.
		// Cap constant length with a generous upper limit that also
		// allows for separators between all digits.
		const limit = 10000
		if len(e.Value) > limit {
			check.errorf(e, InvalidConstVal, "excessively long constant: %s... (%d chars)", e.Value[:10], len(e.Value))
			x.invalidate()
			return
		}
	}
	x.setConst(e.Kind, e.Value)
	if !x.isValid() && e.Kind == token.STRING {
		// The parser has checked the literal's syntax, so Go's
		// strconv.Unquote accepts it. The translated compiler's
		// (self-hosting plan, section 8) rejects only escapes that
		// make the string invalid UTF-8: the same error as below.
		check.errorf(e, InvalidUTF8String, "string literal %s is not valid UTF-8; use a []byte for binary data", e.Value)
		x.invalidate()
		return
	}
	if !x.isValid() {
		// The parser already establishes syntactic correctness.
		// If we reach here it's because of number under-/overflow.
		// TODO(gri) setConst (and in turn the go/constant package)
		// should return an error describing the issue.
		check.errorf(e, InvalidConstVal, "malformed constant: %s", e.Value)
		x.invalidate()
		return
	}
	// Plutonium: every string holds valid UTF-8 (language spec section
	// 4.11), so a literal with escapes that make it invalid is an error.
	if e.Kind == token.STRING && !utf8.ValidString(constant.StringVal(x.val)) {
		check.errorf(e, InvalidUTF8String, "string literal %s is not valid UTF-8; use a []byte for binary data", e.Value)
		x.invalidate()
		return
	}
	// Ensure that integer values don't overflow (go.dev/issue/54280).
	x.expr = e // make sure that check.overflow below has an error position
	check.overflow(x, opPos(x.expr))
}

func (check *Checker) funcLit(x *operand, e *ast.FuncLit) {
	if sig, ok := check.typ(e.Type).(*Signature); ok {
		// Set the Scope's extent to the complete "func (...) {...}"
		// so that Scope.Innermost works correctly.
		sig.scope.pos = e.Pos()
		sig.scope.end = endPos(e)
		if !check.conf.IgnoreFuncBodies && e.Body != nil {
			// Anonymous functions are considered part of the
			// init expression/func declaration which contains
			// them: use existing package-level declaration info.
			decl := check.decl // capture for use in closure below
			iota := check.iota // capture for use in closure below (go.dev/issue/22345)
			// Don't type-check right away because the function may
			// be part of a type definition to which the function
			// body refers. Instead, type-check as soon as possible,
			// but before the enclosing scope contents changes (go.dev/issue/22992).
			check.later(func() {
				check.funcBody(decl, "<function literal>", sig, e.Body, iota)
			}).describef(e, "func literal")
		}
		x.mode_ = value
		x.typ_ = sig
	} else {
		check.errorf(e, InvalidSyntaxTree, "invalid function literal %v", e)
		x.invalidate()
	}
}

func (check *Checker) compositeLit(x *operand, e *ast.CompositeLit, hint Type) {
	var typ, base Type
	var isElem bool // true if composite literal is an element of an enclosing composite literal

	// Plutonium: U.V{...} is a literal of variant V's payload that makes a
	// value of union type U.
	var union Type
	var variant *Variant
	if e.Type != nil {
		if U, v, ok := check.variantLiteralType(e.Type); ok {
			if v == nil {
				check.useLitElts(e)
				x.invalidate()
				return
			}
			union, variant = U, v
		}
	}

	switch {
	case variant != nil:
		typ = variant.typ
		base = typ

	case e.Type != nil:
		// composite literal type present - use it
		// [...]T array types may only appear with composite literals.
		// Check for them here so we don't have to handle ... in general.
		if atyp, _ := e.Type.(*ast.ArrayType); atyp != nil && isdddArray(atyp) {
			// We have an "open" [...]T array type.
			// Create a new ArrayType with unknown length (-1)
			// and finish setting it up after analyzing the literal.
			typ = &Array{len: -1, elem: check.varType(atyp.Elt)}
			base = typ
			break
		}
		typ = check.typ(e.Type)
		base = typ

	case hint != nil:
		// no composite literal type present - use hint (element type of enclosing type)
		typ = hint
		base = typ
		// *T implies &T{}
		u, _ := commonUnder(base, nil)
		if b, ok := deref(u); ok {
			base = b
		}
		isElem = true

	default:
		// TODO(gri) provide better error messages depending on context
		check.error(e, UntypedLit, "missing type in composite literal")
		// continue with invalid type so that elements are "used" (go.dev/issue/69092)
		typ = Typ[Invalid]
		base = typ
	}

	// We cannot create a literal of an incomplete type; make sure it's complete.
	if !check.isComplete(base) {
		x.invalidate()
		return
	}

	// Plutonium: aligned[T, N]{...} and volatile[T]{...} are literals of T,
	// stored with the wrapper's placement.
	for {
		w, ok := base.Underlying().(*Wrapper)
		if !ok || !w.isTransparent() {
			break
		}
		base = w.elem
	}

	switch u, _ := commonUnder(base, nil); utyp := u.(type) {
	case *Wrapper:
		// ring[T, N]{} is the empty ring; a ring has no elements to list.
		if utyp.kind == WrapRing && len(e.Elts) == 0 {
			break
		}
		for _, e := range e.Elts {
			if kv, _ := e.(*ast.KeyValueExpr); kv != nil {
				e = kv.Value
			}
			check.use(e)
		}
		if utyp.kind == WrapRing {
			check.errorf(e.Elts[0], RingLiteralNotEmpty, "ring literal must be empty: %s{}", typ)
		} else {
			check.errorf(e, InvalidLit, "invalid composite literal type %s", typ)
		}
		x.invalidate()
		return

	case *TaggedUnion:
		check.useLitElts(e)
		check.unionLiteralError(e, typ)
		x.invalidate()
		return

	case *Packed:
		if utyp.IsUnion() {
			check.useLitElts(e)
			check.unionLiteralError(e, typ)
			x.invalidate()
			return
		}
		// Packed literals name their fields: field order says nothing
		// about layout a reader can check, so positional values are not
		// allowed.
		seen := make(map[string]bool)
		for _, elt := range e.Elts {
			kv, _ := elt.(*ast.KeyValueExpr)
			if kv == nil {
				check.genericExpr(x, elt, nil)
				check.error(elt, BadPackedLiteral, "packed literal elements must be field: value")
				continue
			}
			check.genericExpr(x, kv.Value, nil)
			key, _ := kv.Key.(*ast.Ident)
			if key == nil {
				check.errorf(kv, BadPackedLiteral, "invalid field name %s in packed literal", kv.Key)
				continue
			}
			i := utyp.lookupField(key.Name)
			if i < 0 || key.Name == "_" {
				check.errorf(kv.Key, MissingLitField, "unknown field %s in packed literal of type %s", key.Name, base)
				continue
			}
			if variant != nil && !check.packedVariantKey(key, variant) {
				continue
			}
			fld := utyp.fields[i].obj
			check.recordUse(key, fld)
			check.assignment(x, fld.typ, "packed literal")
			if seen[key.Name] {
				check.errorf(kv, DuplicateLitField, "duplicate field name %s in packed literal", key.Name)
			}
			seen[key.Name] = true
		}

	case *Struct:
		if len(e.Elts) == 0 {
			break
		}
		// Convention for error messages on invalid struct literals:
		// we mention the struct type only if it clarifies the error
		// (e.g., a duplicate field error doesn't need the struct type).
		fields := utyp.fields
		if _, ok := e.Elts[0].(*ast.KeyValueExpr); ok {
			// all elements must have keys
			visited := newTrie[*Var]()
			for _, e := range e.Elts {
				kv, _ := e.(*ast.KeyValueExpr)
				if kv == nil {
					check.error(e, MixedStructLit, "mixture of field:value and value elements in struct literal")
					continue
				}
				key, _ := kv.Key.(*ast.Ident)
				// do all possible checks early (before exiting due to errors)
				// so we don't drop information on the floor
				check.genericExpr(x, kv.Value, nil)
				if key == nil {
					check.errorf(kv, InvalidLitField, "invalid field name %s in struct literal", kv.Key)
					continue
				}
				obj, index, indirect := lookupFieldOrMethod(utyp, false, check.pkg, key.Name, false)
				if obj == nil {
					alt, _, _ := lookupFieldOrMethod(utyp, false, check.pkg, key.Name, true)
					msg := check.lookupError(base, key.Name, alt, true)
					check.error(kv.Key, MissingLitField, msg)
					continue
				}
				fld, _ := obj.(*Var)
				if fld == nil {
					check.errorf(kv.Key, MissingLitField, "%s is not a field", kv.Key)
					continue
				}
				if len(index) > 1 && !check.verifyVersionf(kv.Key, go1_27, "use of promoted field %s in struct literal of type %s", fieldPath(utyp, index), base) {
					continue
				}
				if indirect {
					check.errorf(kv.Key, InvalidLitField, "invalid implicit pointer indirection to reach %s", kv.Key)
					continue
				}
				check.recordUse(key, fld)
				etyp := fld.typ
				check.assignment(x, etyp, "struct literal")
				if alt, n := visited.insert(index, fld); n != 0 {
					if fld == alt {
						check.errorf(kv, DuplicateLitField, "duplicate field name %s in struct literal", fld.name)
					} else if n < len(index) {
						check.errorf(kv, DuplicateLitField, "cannot specify promoted field %s and enclosing embedded field %s", fld.name, alt.name)
					} else { // n > len(index)
						check.errorf(kv, DuplicateLitField, "cannot specify embedded field %s and enclosed promoted field %s", fld.name, alt.name)
					}
				}
			}
		} else {
			// no element must have a key
			for i, e := range e.Elts {
				if kv, _ := e.(*ast.KeyValueExpr); kv != nil {
					check.error(kv, MixedStructLit, "mixture of field:value and value elements in struct literal")
					continue
				}
				check.genericExpr(x, e, nil)
				if i >= len(fields) {
					check.errorf(x, InvalidStructLit, "too many values in struct literal of type %s", base)
					break // cannot continue
				}
				// i < len(fields)
				fld := fields[i]
				if !fld.Exported() && fld.pkg != check.pkg {
					check.errorf(x, UnexportedLitField, "implicit assignment to unexported field %s in struct literal of type %s", fld.name, base)
					continue
				}
				etyp := fld.typ
				check.assignment(x, etyp, "struct literal")
			}
			if len(e.Elts) < len(fields) {
				var hint string
				for _, fld := range fields {
					if !fld.Exported() && fld.pkg != check.pkg {
						hint = " (type has unexported fields - use key:value pairs)"
						break
					}
				}
				check.errorf(inNode(e, e.Rbrace), InvalidStructLit, "too few values in struct literal of type %s%s", base, hint)
				// ok to continue
			}
		}

	case *Array:
		n := check.indexedElts(e.Elts, utyp.elem, utyp.len)
		// If we have an array of unknown length (usually [...]T arrays, but also
		// arrays [n]T where n is invalid) set the length now that we know it and
		// record the type for the array (usually done by check.typ which is not
		// called for [...]T). We handle [...]T arrays and arrays with invalid
		// length the same here because it makes sense to "guess" the length for
		// the latter if we have a composite literal; e.g. for [n]int{1, 2, 3}
		// where n is invalid for some reason, it seems fair to assume it should
		// be 3 (see also Checked.arrayLength and go.dev/issue/27346).
		if utyp.len < 0 {
			utyp.len = n
			// e.Type is missing if we have a composite literal element
			// that is itself a composite literal with omitted type. In
			// that case there is nothing to record (there is no type in
			// the source at that point).
			if e.Type != nil {
				check.recordTypeAndValue(e.Type, typexpr, utyp, constant.Value{})
			}
		}

	case *Slice:
		check.indexedElts(e.Elts, utyp.elem, -1)

	case *Map:
		// If the map key type is an interface (but not a type parameter),
		// the type of a constant key must be considered when checking for
		// duplicates.
		keyIsInterface := isNonTypeParamInterface(utyp.key)
		visited := make(map[goValue][]Type, len(e.Elts))
		for _, e := range e.Elts {
			kv, _ := e.(*ast.KeyValueExpr)
			if kv == nil {
				check.error(e, MissingLitKey, "missing key in map literal")
				continue
			}
			check.genericExpr(x, kv.Key, utyp.key)
			check.assignment(x, utyp.key, "map literal")
			if !x.isValid() {
				continue
			}
			if x.mode() == constant_ {
				duplicate := false
				xkey := keyVal(x.val)
				if keyIsInterface {
					for _, vtyp := range visited[xkey] {
						if Identical(vtyp, x.typ()) {
							duplicate = true
							break
						}
					}
					visited[xkey] = append(visited[xkey], x.typ())
				} else {
					_, duplicate = visited[xkey]
					visited[xkey] = nil
				}
				if duplicate {
					check.errorf(x, DuplicateLitKey, "duplicate key %s in map literal", x.val)
					continue
				}
			}
			check.genericExpr(x, kv.Value, utyp.elem)
			check.assignment(x, utyp.elem, "map literal")
		}

	default:
		// when "using" all elements unpack KeyValueExpr
		// explicitly because check.use doesn't accept them
		for _, e := range e.Elts {
			if kv, _ := e.(*ast.KeyValueExpr); kv != nil {
				// Ideally, we should also "use" kv.Key but we can't know
				// if it's an externally defined struct key or not. Going
				// forward anyway can lead to other errors. Give up instead.
				e = kv.Value
			}
			check.use(e)
		}
		// if utyp is invalid, an error was reported before
		if isValid(utyp) {
			var qualifier string
			if isElem {
				qualifier = " element"
			}
			var cause string
			if utyp == nil {
				cause = " (no common underlying type)"
			}
			check.errorf(e, InvalidLit, "invalid composite literal%s type %s%s", qualifier, typ, cause)
			x.invalidate()
			return
		}
	}

	x.mode_ = value
	x.typ_ = typ
	if union != nil {
		x.typ_ = union
	}
}

// useLitElts evaluates the elements of a composite literal that is in
// error, so that they count as used.
func (check *Checker) useLitElts(e *ast.CompositeLit) {
	for _, elt := range e.Elts {
		if kv, _ := elt.(*ast.KeyValueExpr); kv != nil {
			elt = kv.Value
		}
		check.use(elt)
	}
}

// indexedElts checks the elements (elts) of an array or slice composite literal
// against the literal's element type (typ), and the element indices against
// the literal length if known (length >= 0). It returns the length of the
// literal (maximum index value + 1).
func (check *Checker) indexedElts(elts []ast.Expr, typ Type, length int64) int64 {
	visited := make(map[int64]bool, len(elts))
	var index, max int64
	for _, e := range elts {
		// determine and check index
		validIndex := false
		eval := e
		if kv, _ := e.(*ast.KeyValueExpr); kv != nil {
			if typ, i := check.index(kv.Key, length); isValid(typ) {
				if i >= 0 {
					index = i
					validIndex = true
				} else {
					check.errorf(e, InvalidLitIndex, "index %s must be integer constant", kv.Key)
				}
			}
			eval = kv.Value
		} else if length >= 0 && index >= length {
			check.errorf(e, OversizeArrayLit, "index %d is out of bounds (>= %d)", index, length)
		} else {
			validIndex = true
		}

		// if we have a valid index, check for duplicate entries
		if validIndex {
			if visited[index] {
				check.errorf(e, DuplicateLitKey, "duplicate index %d in array or slice literal", index)
			}
			visited[index] = true
		}
		index++
		if index > max {
			max = index
		}

		// check element against composite literal element type
		var x operand
		check.genericExpr(&x, eval, typ)
		check.assignment(&x, typ, "array or slice literal")
	}
	return max
}
