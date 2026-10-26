// Translated by go2pu from internal/ssa/print.go at the go-cutover tag; edited by hand since.

package ssa

import (
	"pluto/fmt"
	"pluto/math"
	"selfhost/compat"
	"selfhost/pu"
)

// String formats the function as text, for tests and debugging.
func (f *Func) String() string {
	sb := pu.Alloc[compat.Builder]()
	compat.Fprintfb(sb, "func %s", compat.ArgS(f.Name))
	sb.WriteByte('(')
	for i, t := range f.Params {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(t.String())
	}
	sb.WriteString(")")
	if len(f.Results) > 0 {
		sb.WriteString(" (")
		for i, t := range f.Results {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(t.String())
		}
		sb.WriteString(")")
	}
	sb.WriteByte('\n')
	for _, b := range f.Blocks {
		f.writeBlock(sb, b)
	}
	return sb.String()
}

func (f *Func) writeBlock(sb *compat.Builder, b *Block) {
	compat.Fprintfb(sb, "  %s:", compat.ArgS(compat.StringOf(b)))
	if len(b.Preds) > 0 {
		sb.WriteString(" <-")
		for _, e := range b.Preds {
			compat.Fprintfb(sb, " %s", compat.ArgS(compat.StringOf(e.B)))
		}
	}
	sb.WriteByte('\n')
	for _, v := range b.Values {
		compat.Fprintfb(sb, "    %s\n", compat.ArgS(v.LongString()))
	}
	compat.Fprintfb(sb, "    %s", compat.ArgS((b.Kind).String()))
	for _, c := range b.Controls {
		compat.Fprintfb(sb, " %s", compat.ArgS(compat.StringOf(c)))
	}
	if len(b.Succs) > 0 {
		sb.WriteString(" ->")
		for _, e := range b.Succs {
			compat.Fprintfb(sb, " %s", compat.ArgS(compat.StringOf(e.B)))
		}
	}
	switch b.Likely {
	case Likely:
		sb.WriteString(" (likely)")
	case Unlikely:
		sb.WriteString(" (unlikely)")
	}
	sb.WriteByte('\n')
}

// LongString formats a value with its operation and arguments.
func (v *Value) LongString() string {
	sb := pu.Alloc[compat.Builder]()
	compat.Fprintfb(sb, "%s = %s", compat.ArgS(compat.StringOf(v)), compat.ArgS((v.Op).String()))
	if v.Type != nil {
		compat.Fprintfb(sb, " <%s>", compat.ArgS(compat.StringOf(v.Type)))
	}
	switch v.Op {
	case OpConst, OpArg, OpOffPtr, OpSelectN, OpZero, OpMove:
		compat.Fprintfb(sb, " [%d]", compat.ArgI(v.AuxInt))
	case OpConstFloat:
		fmt.Fprintf(sb, " [%g]", math.Float64frombits(uint64(v.AuxInt)))
	case OpLoad, OpStore:
		if v.AuxInt&MemVolatile != 0 {
			sb.WriteString(" [volatile]")
		}
		if v.AuxInt&MemStatic != 0 {
			sb.WriteString(" [static]")
		}
	default:
		if v.AuxInt != 0 {
			compat.Fprintfb(sb, " [%d]", compat.ArgI(v.AuxInt))
		}
	}
	if v.Aux != nil {
		fmt.Fprintf(sb, " {%v}", v.Aux)
	}
	for _, a := range v.Args {
		compat.Fprintfb(sb, " %s", compat.ArgS(compat.StringOf(a)))
	}
	return sb.String()
}
