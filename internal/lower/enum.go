package lower

// Enum types lower as their underlying integer type. This file builds what
// the checker leaves to lowering: the generated String method, the checked
// comma-ok conversion c, ok := E(n), and range E.

import (
	"encoding/binary"
	"fmt"
	"go/constant"
	"slices"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// enumOfMethod returns the enum whose generated String method fn is, or nil.
func enumOfMethod(fn *types2.Func) *types2.Enum {
	recv := fn.Type().(*types2.Signature).Recv()
	if recv == nil {
		return nil
	}
	if en := types2.EnumOf(recv.Type()); en != nil && en.GeneratedString() == fn {
		return en
	}
	return nil
}

// enumBits returns a member's value as the bit pattern of a 64-bit integer.
func enumBits(c *types2.Const) uint64 {
	if i, ok := constant.Int64Val(c.Val()); ok {
		return uint64(i)
	}
	u, _ := constant.Uint64Val(c.Val())
	return u
}

// enumDense reports whether the members' values are 0, 1, 2, ... in
// declaration order.
func enumDense(en *types2.Enum) bool {
	for i := 0; i < en.Len(); i++ {
		if enumBits(en.Member(i)) != uint64(i) {
			return false
		}
	}
	return true
}

// enumString returns the symbol of an enum's generated String method,
// queueing its body the first time. The body maps the receiver to the
// member's name: through a table of names when the values are close
// together, and through a compare chain otherwise. Values that are not
// members give "E(?)".
func (l *Lowerer) enumString(fn *types2.Func, en *types2.Enum) *ssa.Sym {
	tn := en.Obj()
	name := funcSymName(fn, nil)
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	sig := fn.Type().(*types2.Signature)
	T := sig.Recv().Type()
	strT := types2.Typ[types2.String]
	invalid := tn.Name() + "(?)"

	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		x := s.incomingArgs(sig)[0].v
		res := s.temp(strT, "name")
		s.storeTo(res, s.stringConst(invalid, strT), strT)

		lo, span, ok := enumSpan(en, isUnsigned(T))
		if ok && span <= 2*uint64(en.Len())+8 {
			// Table of names indexed by value - lo, with holes named
			// like invalid values.
			names := make([]string, span)
			for i := range names {
				names[i] = invalid
			}
			for i := 0; i < en.Len(); i++ {
				m := en.Member(i)
				names[enumBits(m)-lo] = m.Name()
			}
			table := s.l.stringTable(sym.Name+"·names", names)
			idx := s.widenEnum(x, T)
			idx = s.newValue(ssa.OpSub, ssa.TI64, idx, s.constInt(ssa.TI64, int64(lo)))
			inRange := s.newValue(ssa.OpLessu, ssa.TBool, idx, s.constInt(ssa.TI64, int64(span)))
			s.ifThen(inRange, func() {
				off := s.newValue(ssa.OpMul, ssa.TI64, idx, s.constInt(ssa.TI64, strSize))
				s.move(res, s.newValue(ssa.OpAddPtr, ssa.TPtr, s.symAddr(table), off), strSize)
			})
		} else {
			wide := s.widenEnum(x, T)
			for i := 0; i < en.Len(); i++ {
				m := en.Member(i)
				eq := s.newValue(ssa.OpEq, ssa.TBool, wide, s.constInt(ssa.TI64, int64(enumBits(m))))
				s.ifThen(eq, func() {
					s.storeTo(res, s.stringConst(m.Name(), strT), strT)
				})
			}
		}
		s.returnValues([]value{aggValue(res, strT)})
	}})
	return sym
}

// enumSpan returns the smallest member value (as a 64-bit pattern) and the
// number of values from it to the largest, in the order of the enum's
// signedness. ok is false if the span does not fit in a uint64.
func enumSpan(en *types2.Enum, unsigned bool) (lo, span uint64, ok bool) {
	less := func(a, b uint64) bool {
		if unsigned {
			return a < b
		}
		return int64(a) < int64(b)
	}
	lo = enumBits(en.Member(0))
	hi := lo
	for i := 1; i < en.Len(); i++ {
		v := enumBits(en.Member(i))
		if less(v, lo) {
			lo = v
		}
		if less(hi, v) {
			hi = v
		}
	}
	span = hi - lo + 1
	return lo, span, span != 0
}

// widenEnum widens an enum (or integer) value of type T to 64 bits,
// sign- or zero-extending by T's signedness.
func (s *fnState) widenEnum(x *ssa.Value, T types2.Type) *ssa.Value {
	if isUnsigned(T) {
		return s.convertScalar(x, T, types2.Typ[types2.Uint64])
	}
	return s.convertScalar(x, T, types2.Typ[types2.Int64])
}

// stringTable returns a read-only array of string headers.
func (l *Lowerer) stringTable(name string, strs []string) *ssa.Sym {
	sym := l.out.Sym(name, ssa.SymData)
	buf := make([]byte, strSize*len(strs))
	d := &ssa.Data{Sym: sym, Size: int64(len(buf)), Align: 8, Bytes: buf, ReadOnly: true, Local: true}
	for i, str := range strs {
		l.putStaticString(d, int64(strSize*i), str)
	}
	l.out.Data = append(l.out.Data, d)
	return sym
}

// isEnumConversion reports whether e is a conversion to an enum type from
// a value of another type: the comma-ok form c, ok := E(n).
func (s *fnState) isEnumConversion(e *ast.CallExpr) bool {
	tv := s.info.Types[e.Fun]
	if !tv.IsType() || len(e.Args) != 1 {
		return false
	}
	T := s.subst(tv.Type)
	return types2.EnumOf(T) != nil && !types2.Identical(T, s.subst(s.typeOf(e.Args[0])))
}

// enumConversion lowers c, ok := E(n): ok reports whether n equals a
// member's value, and c is that member, or the zero value if ok is false.
// The test is a range check per run of consecutive member values.
func (s *fnState) enumConversion(e *ast.CallExpr) (value, *ssa.Value) {
	T := s.subst(s.info.Types[e.Fun].Type)
	en := types2.EnumOf(T)
	F := s.subst(s.typeOf(e.Args[0]))
	n := s.exprAs(e.Args[0], F).v
	wide := s.widenEnum(n, F)

	// The members n can equal, as patterns of n widened to 64 bits.
	size := s.l.sizeof(F) * 8
	unsigned := isUnsigned(F)
	var pats []uint64
	for i := 0; i < en.Len(); i++ {
		v := en.Member(i).Val()
		if p, ok := fitsInt(v, size, unsigned); ok {
			pats = append(pats, p)
		}
	}
	slices.SortFunc(pats, func(a, b uint64) int {
		if unsigned {
			return cmpUint(a, b)
		}
		return cmpUint(a^1<<63, b^1<<63) // signed order
	})

	ok := s.constBool(false)
	for i := 0; i < len(pats); {
		j := i
		for j+1 < len(pats) && pats[j+1] == pats[j]+1 {
			j++
		}
		lo, hi := pats[i], pats[j]
		var in *ssa.Value
		if lo == hi {
			in = s.newValue(ssa.OpEq, ssa.TBool, wide, s.constInt(ssa.TI64, int64(lo)))
		} else {
			d := s.newValue(ssa.OpSub, ssa.TI64, wide, s.constInt(ssa.TI64, int64(lo)))
			in = s.newValue(ssa.OpLequ, ssa.TBool, d, s.constInt(ssa.TI64, int64(hi-lo)))
		}
		ok = s.newValue(ssa.OpOr, ssa.TBool, ok, in)
		i = j + 1
	}

	t := s.l.scalarType(T)
	c := s.convertScalar(n, F, T)
	c = s.newValue(ssa.OpSelect, t, ok, c, s.constInt(t, 0))
	return scalarValue(c, T), ok
}

// fitsInt reports whether constant v is representable in an integer of the
// given size and signedness, and returns its value as a 64-bit pattern.
func fitsInt(v constant.Value, size int64, unsigned bool) (uint64, bool) {
	if unsigned {
		u, ok := constant.Uint64Val(v)
		if !ok || constant.Sign(v) < 0 {
			return 0, false
		}
		return u, size == 64 || u < 1<<uint(size)
	}
	i, ok := constant.Int64Val(v)
	if !ok {
		return 0, false
	}
	return uint64(i), size == 64 || -1<<(size-1) <= i && i < 1<<(size-1)
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// rangeEnum lowers "for c := range E": the members of E in declaration
// order, counted directly when they are 0, 1, 2, ..., and read from a
// table of values otherwise.
func (s *fnState) rangeEnum(st *ast.RangeStmt, label *types2.Label) {
	T := s.subst(s.info.Types[st.X].Type)
	en := types2.EnumOf(T)
	intT := types2.Typ[types2.Int]
	keyLV := s.rangeVar(st, st.Key)
	n := s.constInt(ssa.TI64, int64(en.Len()))

	var table *ssa.Sym
	t := s.l.scalarType(T)
	if !enumDense(en) {
		table = s.l.enumValues(en, t)
	}
	s.countedLoop(n, label, func(i *ssa.Value) {
		if keyLV != nil {
			var v *ssa.Value
			if table == nil {
				v = s.convertScalar(i, intT, T)
			} else {
				off := s.newValue(ssa.OpMul, ssa.TI64, i, s.constInt(ssa.TI64, t.Size))
				v = s.load(t, s.newValue(ssa.OpAddPtr, ssa.TPtr, s.symAddr(table), off))
			}
			s.storeLV(*keyLV, s.assignConvert(scalarValue(v, T), unwrap(keyLV.T), st))
		}
		s.block(st.Body, st.Body.List)
	})
}

// enumValues returns a read-only table of an enum's member values in
// declaration order, each of scalar type t.
func (l *Lowerer) enumValues(en *types2.Enum, t *ssa.Type) *ssa.Sym {
	sym := l.out.Sym(fmt.Sprintf(".enumvalues.%d", l.statics), ssa.SymData)
	l.statics++
	buf := make([]byte, int(t.Size)*en.Len())
	for i := 0; i < en.Len(); i++ {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], enumBits(en.Member(i)))
		copy(buf[i*int(t.Size):], b[:t.Size])
	}
	l.out.Data = append(l.out.Data, &ssa.Data{Sym: sym, Size: int64(len(buf)), Align: t.Size, Bytes: buf, ReadOnly: true, Local: true})
	return sym
}
