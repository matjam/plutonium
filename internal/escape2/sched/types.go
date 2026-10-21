package sched

import (
	"github.com/matjam/plutonium/internal/types2"
)

// typeID names a type in a typeTable: identical types (types2.Identical)
// have the same ID. No type is keyed by its text (plan section (b), stage B;
// review R3 §4): the text only picks the bucket, and identity decides.
type typeID int32

// typeTable is the canonical type interner of stage B.
type typeTable struct {
	types   []types2.Type
	buckets map[string][]typeID
	hasTP   map[types2.Type]bool
}

func newTypeTable() *typeTable {
	return &typeTable{buckets: map[string][]typeID{}, hasTP: map[types2.Type]bool{}}
}

// intern returns the ID of t, adding it if no identical type is known.
func (tt *typeTable) intern(t types2.Type) typeID {
	if id, ok := tt.lookup(t); ok {
		return id
	}
	h := bucketKey(t)
	id := typeID(len(tt.types))
	tt.types = append(tt.types, t)
	tt.buckets[h] = append(tt.buckets[h], id)
	return id
}

// lookup returns the ID of a type identical to t, if one is interned.
func (tt *typeTable) lookup(t types2.Type) (typeID, bool) {
	for _, id := range tt.buckets[bucketKey(t)] {
		if types2.Identical(tt.types[id], t) {
			return id, true
		}
	}
	return 0, false
}

// bucketKey is a text that identical types (types2.Identical) always share:
// the type's structure, without what identity ignores (names of parameters
// and results) or what this text need not tell apart (struct tags, the
// scopes of local named types), which Identical decides within the bucket.
// TypeString is not such a text: it prints parameter names.
func bucketKey(t types2.Type) string {
	var b []byte
	return string(appendKey(b, t, 0))
}

func appendKey(b []byte, t types2.Type, depth int) []byte {
	if depth > 32 {
		return append(b, '~')
	}
	d := depth + 1
	switch t := types2.Unalias(t).(type) {
	case nil:
		return append(b, "nil"...)
	case *types2.Named:
		o := t.Origin().Obj()
		if o.Pkg() != nil {
			b = append(b, o.Pkg().Path()...)
			b = append(b, '.')
		}
		b = append(b, o.Name()...)
		if args := t.TypeArgs(); args != nil && args.Len() > 0 {
			b = append(b, '[')
			for i := 0; i < args.Len(); i++ {
				b = appendKey(b, args.At(i), d)
				b = append(b, ',')
			}
			b = append(b, ']')
		}
		return b
	case *types2.TypeParam:
		return append(append(b, "$"...), t.Obj().Name()...)
	case *types2.Pointer:
		return appendKey(append(b, '*'), t.Elem(), d)
	case *types2.Slice:
		return appendKey(append(b, "[]"...), t.Elem(), d)
	case *types2.Array:
		b = append(b, '[')
		b = appendInt(b, t.Len())
		return appendKey(append(b, ']'), t.Elem(), d)
	case *types2.Map:
		b = appendKey(append(b, "map["...), t.Key(), d)
		return appendKey(append(b, ']'), t.Elem(), d)
	case *types2.Chan:
		b = append(b, "chan"...)
		b = appendInt(b, int64(t.Dir()))
		return appendKey(append(b, ' '), t.Elem(), d)
	case *types2.Wrapper:
		b = append(b, "wrap"...)
		b = appendInt(b, int64(t.Kind()))
		return appendKey(append(b, '['), t.Elem(), d)
	case *types2.Signature:
		b = append(b, "func("...)
		for i := 0; i < t.Params().Len(); i++ {
			b = appendKey(b, t.Params().At(i).Type(), d)
			b = append(b, ',')
		}
		if t.Variadic() {
			b = append(b, "..."...)
		}
		b = append(b, ")("...)
		for i := 0; i < t.Results().Len(); i++ {
			b = appendKey(b, t.Results().At(i).Type(), d)
			b = append(b, ',')
		}
		return append(b, ')')
	case *types2.Tuple:
		b = append(b, '(')
		for i := 0; i < t.Len(); i++ {
			b = appendKey(b, t.At(i).Type(), d)
			b = append(b, ',')
		}
		return append(b, ')')
	case *types2.Struct:
		b = append(b, "struct{"...)
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if f.Embedded() {
				b = append(b, '!')
			}
			b = append(b, f.Name()...)
			b = append(b, ' ')
			b = appendKey(b, f.Type(), d)
			b = append(b, ';')
		}
		return append(b, '}')
	case *types2.Interface:
		// Identical interfaces have the same method names; their
		// signatures are compared by Identical.
		b = append(b, "interface{"...)
		for i := 0; i < t.NumMethods(); i++ {
			b = append(b, t.Method(i).Name()...)
			b = append(b, ';')
		}
		return append(b, '}')
	}
	return append(b, types2.TypeString(t, nil)...)
}

func appendInt(b []byte, n int64) []byte {
	if n < 0 {
		b = append(b, '-')
		n = -n
	}
	var tmp [20]byte
	i := len(tmp)
	for {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	return append(b, tmp[i:]...)
}

// hasTypeParam reports whether t mentions a type parameter.
func (tt *typeTable) hasTypeParam(t types2.Type) bool {
	if v, ok := tt.hasTP[t]; ok {
		return v
	}
	tt.hasTP[t] = false // cycles through named types: assume none until found
	v := false
	walkType(t, func(u types2.Type) bool {
		if _, ok := u.(*types2.TypeParam); ok {
			v = true
		}
		return !v
	})
	tt.hasTP[t] = v
	return v
}

// typeParamsIn returns the type parameters t mentions, each once, in the
// order found.
func typeParamsIn(t types2.Type) []*types2.TypeParam {
	var out []*types2.TypeParam
	seen := map[*types2.TypeParam]bool{}
	walkType(t, func(u types2.Type) bool {
		if tp, ok := u.(*types2.TypeParam); ok && !seen[tp] {
			seen[tp] = true
			out = append(out, tp)
		}
		return true
	})
	return out
}

// walkType calls visit for t and the types it is built from, not entering a
// named type's underlying type (its type arguments are entered). visit
// returns false to stop the walk.
func walkType(t types2.Type, visit func(types2.Type) bool) bool {
	if t == nil {
		return true
	}
	if !visit(t) {
		return false
	}
	switch t := t.(type) {
	case *types2.Alias:
		return walkType(types2.Unalias(t), visit)
	case *types2.Named:
		if args := t.TypeArgs(); args != nil {
			for i := 0; i < args.Len(); i++ {
				if !walkType(args.At(i), visit) {
					return false
				}
			}
		}
	case *types2.Pointer:
		return walkType(t.Elem(), visit)
	case *types2.Slice:
		return walkType(t.Elem(), visit)
	case *types2.Array:
		return walkType(t.Elem(), visit)
	case *types2.Map:
		return walkType(t.Key(), visit) && walkType(t.Elem(), visit)
	case *types2.Chan:
		return walkType(t.Elem(), visit)
	case *types2.Wrapper:
		return walkType(t.Elem(), visit)
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !walkType(t.Field(i).Type(), visit) {
				return false
			}
		}
	case *types2.Tuple:
		for i := 0; i < t.Len(); i++ {
			if !walkType(t.At(i).Type(), visit) {
				return false
			}
		}
	case *types2.Signature:
		return walkType(t.Params(), visit) && walkType(t.Results(), visit)
	case *types2.Interface:
		for i := 0; i < t.NumExplicitMethods(); i++ {
			if !walkType(t.ExplicitMethod(i).Type(), visit) {
				return false
			}
		}
	case *types2.TaggedUnion:
		for i := 0; i < t.NumVariants(); i++ {
			if v := t.Variant(i); v.HasPayload() && !walkType(v.Payload(), visit) {
				return false
			}
		}
	}
	return true
}

// typeDepth is the nesting depth of t's type terms, at most limit+1.
func typeDepth(t types2.Type, limit int) int {
	if limit < 0 || t == nil {
		return 0
	}
	max := 0
	sub := func(u types2.Type) {
		if d := typeDepth(u, limit-1); d > max {
			max = d
		}
	}
	switch t := types2.Unalias(t).(type) {
	case *types2.Named:
		if args := t.TypeArgs(); args != nil {
			for i := 0; i < args.Len(); i++ {
				sub(args.At(i))
			}
		}
	case *types2.Pointer:
		sub(t.Elem())
	case *types2.Slice:
		sub(t.Elem())
	case *types2.Array:
		sub(t.Elem())
	case *types2.Map:
		sub(t.Key())
		sub(t.Elem())
	case *types2.Chan:
		sub(t.Elem())
	case *types2.Wrapper:
		sub(t.Elem())
	case *types2.Signature:
		for i := 0; i < t.Params().Len(); i++ {
			sub(t.Params().At(i).Type())
		}
		for i := 0; i < t.Results().Len(); i++ {
			sub(t.Results().At(i).Type())
		}
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			sub(t.Field(i).Type())
		}
	}
	return max + 1
}

// mayMatch reports whether a and b may be the same type once their type
// parameters are instantiated: a type parameter matches anything. It
// over-approximates (a false answer is always right), which is what the
// candidate sets need: a call of a generic signature may reach any function
// whose signature some instantiation makes identical.
func (tt *typeTable) mayMatch(a, b types2.Type) bool {
	return tt.mayMatchDepth(a, b, 0)
}

func (tt *typeTable) mayMatchDepth(a, b types2.Type, depth int) bool {
	if a == nil || b == nil {
		return a == b
	}
	a, b = types2.Unalias(a), types2.Unalias(b)
	if _, ok := a.(*types2.TypeParam); ok {
		return true
	}
	if _, ok := b.(*types2.TypeParam); ok {
		return true
	}
	if !tt.hasTypeParam(a) && !tt.hasTypeParam(b) {
		return types2.Identical(a, b)
	}
	if depth > 16 {
		return true // deep generic terms: assume they may match
	}
	d := depth + 1
	switch a := a.(type) {
	case *types2.Named:
		b, ok := b.(*types2.Named)
		if !ok || a.Origin() != b.Origin() {
			return false
		}
		aa, ba := a.TypeArgs(), b.TypeArgs()
		if aa.Len() != ba.Len() {
			return false
		}
		for i := 0; i < aa.Len(); i++ {
			if !tt.mayMatchDepth(aa.At(i), ba.At(i), d) {
				return false
			}
		}
		return true
	case *types2.Pointer:
		b, ok := b.(*types2.Pointer)
		return ok && tt.mayMatchDepth(a.Elem(), b.Elem(), d)
	case *types2.Slice:
		b, ok := b.(*types2.Slice)
		return ok && tt.mayMatchDepth(a.Elem(), b.Elem(), d)
	case *types2.Array:
		b, ok := b.(*types2.Array)
		return ok && a.Len() == b.Len() && tt.mayMatchDepth(a.Elem(), b.Elem(), d)
	case *types2.Map:
		b, ok := b.(*types2.Map)
		return ok && tt.mayMatchDepth(a.Key(), b.Key(), d) && tt.mayMatchDepth(a.Elem(), b.Elem(), d)
	case *types2.Chan:
		b, ok := b.(*types2.Chan)
		return ok && a.Dir() == b.Dir() && tt.mayMatchDepth(a.Elem(), b.Elem(), d)
	case *types2.Wrapper:
		b, ok := b.(*types2.Wrapper)
		return ok && a.Kind() == b.Kind() && tt.mayMatchDepth(a.Elem(), b.Elem(), d)
	case *types2.Struct:
		b, ok := b.(*types2.Struct)
		if !ok || a.NumFields() != b.NumFields() {
			return false
		}
		for i := 0; i < a.NumFields(); i++ {
			fa, fb := a.Field(i), b.Field(i)
			if fa.Name() != fb.Name() || fa.Embedded() != fb.Embedded() || !tt.mayMatchDepth(fa.Type(), fb.Type(), d) {
				return false
			}
		}
		return true
	case *types2.Tuple:
		b, ok := b.(*types2.Tuple)
		if !ok || a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !tt.mayMatchDepth(a.At(i).Type(), b.At(i).Type(), d) {
				return false
			}
		}
		return true
	case *types2.Signature:
		b, ok := b.(*types2.Signature)
		return ok && tt.sigMayMatch(a, b, d)
	}
	// Interfaces and the rest with type parameters inside: assume a match.
	return true
}

// sigMayMatch compares two signatures, receivers left out, as mayMatch does.
func (tt *typeTable) sigMayMatch(a, b *types2.Signature, depth int) bool {
	if a.Variadic() != b.Variadic() || a.Params().Len() != b.Params().Len() || a.Results().Len() != b.Results().Len() {
		return false
	}
	return tt.mayMatchDepth(a.Params(), b.Params(), depth) && tt.mayMatchDepth(a.Results(), b.Results(), depth)
}
