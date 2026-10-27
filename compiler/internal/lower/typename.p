// Translated by go2pu from internal/lower/typename.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/types2"
	"pluto/strconv"
)

// reflectName returns the name of T in a type descriptor: T as Go's
// reflect.Type.String writes it, so that %T and %#v print what Go prints.
// Named types are qualified by their package's name (main.T, hash.Map[string,int]),
// byte and rune are uint8 and int32, and any is interface {}. Types Go does
// not have are written as the type checker writes them.
func reflectName(T types2.Type) string {
	var b compat.Builder
	writeReflectName(&b, T)
	return b.String()
}

func writeReflectName(b *compat.Builder, T types2.Type) {
	switch t := types2.Unalias(T).(type) {
	case *types2.Named:
		obj := t.Obj()
		if isErrorString(obj) {
			b.WriteByte('*') // Go's errors.New returns a *errors.errorString
		}
		if obj.Pkg() != nil {
			b.WriteString(obj.Pkg().Name())
			b.WriteByte('.')
		}
		b.WriteString(obj.Name())
		if args := t.TypeArgs(); args != nil && args.Len() > 0 {
			b.WriteByte('[')
			for i := 0; i < args.Len(); i++ {
				if i > 0 {
					b.WriteByte(',')
				}
				writeReflectName(b, args.At(i))
			}
			b.WriteByte(']')
		}
	case *types2.Basic:
		switch t.Kind() {
		case types2.Uint8:
			b.WriteString("uint8")
		case types2.Int32:
			b.WriteString("int32")
		case types2.UnsafePointer:
			b.WriteString("unsafe.Pointer")
		default:
			b.WriteString(t.Name())
		}
	case *types2.Pointer:
		b.WriteByte('*')
		writeReflectName(b, t.Elem())
	case *types2.Slice:
		b.WriteString("[]")
		writeReflectName(b, t.Elem())
	case *types2.Array:
		b.WriteByte('[')
		b.WriteString(strconv.FormatInt(t.Len(), 10))
		b.WriteByte(']')
		writeReflectName(b, t.Elem())
	case *types2.Map:
		b.WriteString("map[")
		writeReflectName(b, t.Key())
		b.WriteByte(']')
		writeReflectName(b, t.Elem())
	case *types2.Signature:
		b.WriteString("func")
		writeReflectSig(b, t)
	case *types2.Struct:
		if t.NumFields() == 0 {
			b.WriteString("struct {}")
			return
		}
		b.WriteString("struct { ")
		for i := 0; i < t.NumFields(); i++ {
			if i > 0 {
				b.WriteString("; ")
			}
			f := t.Field(i)
			if !f.Embedded() {
				b.WriteString(f.Name())
				b.WriteByte(' ')
			}
			writeReflectName(b, f.Type())
			if tag := t.Tag(i); tag != "" {
				b.WriteByte(' ')
				b.WriteString(strconv.Quote(tag))
			}
		}
		b.WriteString(" }")
	case *types2.Interface:
		if t.NumMethods() == 0 {
			b.WriteString("interface {}")
			return
		}
		b.WriteString("interface { ")
		for i := 0; i < t.NumMethods(); i++ {
			if i > 0 {
				b.WriteString("; ")
			}
			m := t.Method(i)
			b.WriteString(m.Name())
			writeReflectSig(b, m.Signature())
		}
		b.WriteString(" }")
	default:
		b.WriteString(types2.TypeString(T, func(p *types2.Package) string { return p.Name() }))
	}
}

// writeReflectSig writes a signature's parameters and results as reflect
// does: (int, ...string) (bool, error).
func writeReflectSig(b *compat.Builder, sig *types2.Signature) {
	b.WriteByte('(')
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		pt := params.At(i).Type()
		if sig.Variadic() && i == params.Len()-1 {
			b.WriteString("...")
			if s, ok := pt.(*types2.Slice); ok {
				pt = s.Elem()
			}
		}
		writeReflectName(b, pt)
	}
	b.WriteByte(')')
	res := sig.Results()
	switch res.Len() {
	case 0:
	case 1:
		b.WriteByte(' ')
		writeReflectName(b, res.At(0).Type())
	default:
		b.WriteString(" (")
		for i := 0; i < res.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			writeReflectName(b, res.At(i).Type())
		}
		b.WriteByte(')')
	}
}
