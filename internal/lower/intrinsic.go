package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Intrinsics: standard library functions the compiler implements directly
// with one SSA operation. Each library function also has a Plutonium body,
// used when its address is taken.
type intrinsicFn func(s *fnState, args []*ssa.Value, results []types2.Type) []*ssa.Value

func unary(op ssa.Op) intrinsicFn {
	return func(s *fnState, args []*ssa.Value, results []types2.Type) []*ssa.Value {
		t := s.l.scalarType(results[0])
		return []*ssa.Value{s.newValue(op, t, args[0])}
	}
}

// bitScan returns an intrinsic for TrailingZeros / LeadingZeros, whose
// result is an int while the operation works at the argument's width.
func bitScan(op ssa.Op) intrinsicFn {
	return func(s *fnState, args []*ssa.Value, results []types2.Type) []*ssa.Value {
		v := s.newValue(op, args[0].Type, args[0])
		if v.Type.Size < 8 {
			v = s.newValue(ssa.OpZeroExt, ssa.TI64, v)
		}
		return []*ssa.Value{v}
	}
}

var intrinsics = map[string]intrinsicFn{
	"pluto/math.Sqrt":            unary(ssa.OpFSqrt),
	"pluto/math.Abs":             unary(ssa.OpFAbs),
	"pluto/math.Float64bits":     unary(ssa.OpBitcast),
	"pluto/math.Float64frombits": unary(ssa.OpBitcast),
	"pluto/math.Float32bits":     unary(ssa.OpBitcast),
	"pluto/math.Float32frombits": unary(ssa.OpBitcast),

	"pluto/bits.TrailingZeros64": bitScan(ssa.OpCtz),
	"pluto/bits.TrailingZeros32": bitScan(ssa.OpCtz),
	"pluto/bits.TrailingZeros":   bitScan(ssa.OpCtz),
	"pluto/bits.LeadingZeros64":  bitScan(ssa.OpClz),
	"pluto/bits.LeadingZeros32":  bitScan(ssa.OpClz),
	"pluto/bits.LeadingZeros":    bitScan(ssa.OpClz),
	"pluto/bits.ReverseBytes64":  unary(ssa.OpBswap),
	"pluto/bits.ReverseBytes32":  unary(ssa.OpBswap),
	"pluto/bits.ReverseBytes16":  unary(ssa.OpBswap),
	"pluto/bits.RotateLeft64": func(s *fnState, args []*ssa.Value, _ []types2.Type) []*ssa.Value {
		return []*ssa.Value{s.newValue(ssa.OpRotl, ssa.TI64, args[0], args[1])}
	},
	"pluto/bits.RotateLeft32": func(s *fnState, args []*ssa.Value, _ []types2.Type) []*ssa.Value {
		return []*ssa.Value{s.newValue(ssa.OpRotl, ssa.TI32, args[0], args[1])}
	},
	"pluto/bits.Mul64": func(s *fnState, args []*ssa.Value, _ []types2.Type) []*ssa.Value {
		hi := s.newValue(ssa.OpMulHiu, ssa.TI64, args[0], args[1])
		lo := s.newValue(ssa.OpMul, ssa.TI64, args[0], args[1])
		return []*ssa.Value{hi, lo}
	},
}

// x86Path is the import path of the x86 intrinsics package.
const x86Path = "pluto/arch/x86"

func init() {
	// Every function of pluto/arch/x86 except HandlerAddr (handled in
	// intrinsic) is one OpIntrinsic named after it.
	for _, name := range []string{
		"Cli", "Sti", "StiHlt", "Hlt", "Pause", "SaveFlagsCli", "RestoreFlags",
		"Inb", "Inw", "Inl", "Outb", "Outw", "Outl",
		"Rdmsr", "Wrmsr",
		"ReadCR0", "ReadCR2", "ReadCR3", "ReadCR4", "WriteCR0", "WriteCR3", "WriteCR4", "Invlpg",
		"Lgdt", "Lidt", "Ltr",
		"Cpuid", "Rdtsc", "Rdtscp",
		"Swapgs", "ReadGSBase", "WriteGSBase", "GSLoad64", "GSLoad32", "GSStore64", "GSStore32",
		"FPUBegin", "FPUEnd",
	} {
		intrinsics[x86Path+"."+name] = machine("x86." + name)
	}
}

// machine returns an intrinsic that expands to the named machine
// instruction sequence: an OpIntrinsic, which orders memory as a call
// does, so that it is a compiler barrier.
func machine(name string) intrinsicFn {
	return func(s *fnState, args []*ssa.Value, results []types2.Type) []*ssa.Value {
		types := []*ssa.Type{ssa.TMem}
		for _, r := range results {
			types = append(types, s.l.scalarType(r))
		}
		v := s.newValue(ssa.OpIntrinsic, ssa.TupleOf(types...), args...)
		v.AddArg(s.mem())
		v.Aux = ssa.Intrinsic(name)
		s.setMem(s.selectN(v, 0, ssa.TMem))
		out := make([]*ssa.Value, len(results))
		for i := range results {
			out[i] = s.selectN(v, i+1, types[i+1])
		}
		return out
	}
}

// handlerAddr lowers x86.HandlerAddr(h): the address of the
// //pu:interrupt function h names.
func (s *fnState) handlerAddr(e *ast.CallExpr) []value {
	uintptrT := types2.Typ[types2.Uintptr]
	var fn *types2.Func
	if len(e.Args) == 1 {
		if id := identOf(ast.Unparen(e.Args[0])); id != nil {
			fn, _ = s.info.Uses[id].(*types2.Func)
		}
	}
	d := s.l.decls[fn]
	if fn == nil || d == nil || !hasDirective(d.decl.Doc, "interrupt") {
		s.l.errorf(e.Pos(), "x86.HandlerAddr needs the name of a //pu:interrupt function")
		return []value{scalarValue(s.constInt(ssa.TI64, 0), uintptrT)}
	}
	addr := s.funcAddr(s.l.funcSym(fn, nil))
	return []value{scalarValue(s.newValue(ssa.OpBitcast, ssa.TI64, addr), uintptrT)}
}

// hasDirective reports whether a doc comment holds //pu:name.
func hasDirective(doc *ast.CommentGroup, name string) bool {
	for _, d := range directives(doc) {
		if d == name {
			return true
		}
	}
	return false
}

// isIntrinsicOnly reports whether fn can only be called directly, never
// used as a value.
func isIntrinsicOnly(fn *types2.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == x86Path && fn.Name() == "HandlerAddr"
}

// intrinsic lowers a call to an intrinsic function, if fn is one.
func (s *fnState) intrinsic(fn *types2.Func, e *ast.CallExpr) ([]value, bool) {
	if fn.Pkg() == nil {
		return nil, false
	}
	if isIntrinsicOnly(fn) {
		return s.handlerAddr(e), true
	}
	impl := intrinsics[fn.Pkg().Path()+"."+fn.Name()]
	if impl == nil {
		return nil, false
	}
	sig := fn.Type().(*types2.Signature)
	args := make([]*ssa.Value, len(e.Args))
	for i, a := range e.Args {
		args[i] = s.exprAs(a, sig.Params().At(i).Type()).v
	}
	var resultTypes []types2.Type
	for i := 0; i < sig.Results().Len(); i++ {
		resultTypes = append(resultTypes, sig.Results().At(i).Type())
	}
	vals := impl(s, args, resultTypes)
	out := make([]value, len(vals))
	for i, v := range vals {
		out[i] = scalarValue(v, resultTypes[i])
	}
	return out, true
}
