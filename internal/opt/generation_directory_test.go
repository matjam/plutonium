package opt

import (
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

func TestGenerationDirectoryPanicExtent(t *testing.T) {
	for _, typ := range []*ssa.Type{ssa.TI8, ssa.TI64} {
		t.Run(typ.String(), func(t *testing.T) {
			p := ssa.NewProgram()
			f := ssa.NewFunc("extent_panic")
			f.Params, f.Results = []*ssa.Type{ssa.TPtr}, []*ssa.Type{typ}
			f.Pragmas = ssa.PragmaGenCheck
			f.GenLive = p.Sym("pluto/rt.GenRegisteredLive", ssa.SymFunc)
			f.GenRange = p.Sym("pluto/rt.GenRegisteredRangeLive", ssa.SymFunc)
			f.GenPanic = p.Sym("pluto/rt.panicStale", ssa.SymFunc)
			f.GenRangePanic = p.Sym("pluto/rt.panicStaleRange", ssa.SymFunc)
			b := f.Entry
			m := b.NewValue(ssa.OpInitMem, ssa.TMem)
			ptr := b.NewValueI(ssa.OpArg, ssa.TPtr, 0)
			v := b.NewValue(ssa.OpLoad, typ, ptr, m)
			b.Kind = ssa.BlockRet
			b.SetControl(m, v)
			GenCheck(f)
			if err := f.Verify(); err != nil {
				t.Fatal(err)
			}
			panics := 0
			for _, block := range f.Blocks {
				for _, call := range block.Values {
					if call.Op != ssa.OpCall || call.Aux != f.GenPanic && call.Aux != f.GenRangePanic {
						continue
					}
					panics++
					if typ == ssa.TI8 {
						if call.Aux != f.GenPanic || len(call.Args) != 2 {
							t.Fatalf("scalar panic changed its operands:\n%s", f)
						}
					} else if call.Aux != f.GenRangePanic || len(call.Args) != 3 || call.Args[1].Op != ssa.OpConst || call.Args[1].AuxInt != 8 {
						t.Fatalf("range panic lost its failed byte extent:\n%s", f)
					}
				}
			}
			if panics != 1 {
				t.Fatalf("want one failure path, got %d:\n%s", panics, f)
			}
		})
	}
}

// A zero-width memory operation touches no address, including when the
// pointer would otherwise need a generation check.
func TestGenerationDirectoryZeroWidth(t *testing.T) {
	p := ssa.NewProgram()
	f := ssa.NewFunc("zero")
	f.Params = []*ssa.Type{ssa.TPtr}
	f.Pragmas = ssa.PragmaGenCheck
	f.GenLive = p.Sym("pluto/rt.GenRegisteredLive", ssa.SymFunc)
	f.GenRange = p.Sym("pluto/rt.GenRegisteredRangeLive", ssa.SymFunc)
	f.GenPanic = p.Sym("pluto/rt.panicStale", ssa.SymFunc)
	f.GenRangePanic = p.Sym("pluto/rt.panicStaleRange", ssa.SymFunc)
	b := f.Entry
	m := b.NewValue(ssa.OpInitMem, ssa.TMem)
	ptr := b.NewValueI(ssa.OpArg, ssa.TPtr, 0)
	m = b.NewValueI(ssa.OpZero, ssa.TMem, 0, ptr, m)
	b.Kind = ssa.BlockRet
	b.SetControl(m)
	GenCheck(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	for _, block := range f.Blocks {
		for _, v := range block.Values {
			if v.Op == ssa.OpCall {
				t.Fatalf("zero-width operation added a check:\n%s", f)
			}
		}
	}
}

func TestGenerationDirectoryExtentReuse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		first   *ssa.Type
		offset  int64
		barrier string
		want    int
	}{
		{"equal extent", ssa.TI64, 0, "", 1},
		{"owned extent", ssa.TI64, 0, "", 1},
		{"different extent", ssa.TI8, 0, "", 2},
		{"different address", ssa.TI64, 16, "", 2},
		{"custom free", ssa.TI64, 0, "pluto/rt.GenAfterCustomFree", 2},
		{"unknown call", ssa.TI64, 0, "external.unknown", 2},
		{"reset", ssa.TI64, 0, "pluto/rt.GenSetEpoch", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := ssa.NewProgram()
			f := ssa.NewFunc("extent")
			f.Params, f.Results = []*ssa.Type{ssa.TPtr}, []*ssa.Type{ssa.TI64}
			f.Pragmas = ssa.PragmaGenCheck
			if tc.name == "owned extent" {
				f.GenOwnerArgs = []int64{0}
			}
			f.GenLive = p.Sym("pluto/rt.GenRegisteredLive", ssa.SymFunc)
			f.GenRange = p.Sym("pluto/rt.GenRegisteredRangeLive", ssa.SymFunc)
			f.GenPanic = p.Sym("pluto/rt.panicStale", ssa.SymFunc)
			f.GenRangePanic = p.Sym("pluto/rt.panicStaleRange", ssa.SymFunc)
			b := f.Entry
			m := b.NewValue(ssa.OpInitMem, ssa.TMem)
			ptr := b.NewValueI(ssa.OpArg, ssa.TPtr, 0)
			first := b.NewValue(ssa.OpLoad, tc.first, ptr, m)
			if tc.first != ssa.TI64 {
				first = b.NewValue(ssa.OpZeroExt, ssa.TI64, first)
			}
			if tc.barrier != "" {
				c := b.NewValueA(ssa.OpCall, ssa.TupleOf(ssa.TMem), p.Sym(tc.barrier, ssa.SymFunc), ptr, m)
				c.Call = &ssa.CallInfo{ABI: ssa.ABIInternal}
				m = b.NewValueI(ssa.OpSelectN, ssa.TMem, 0, c)
			}
			other := ptr
			if tc.offset != 0 {
				other = b.NewValueI(ssa.OpOffPtr, ssa.TPtr, tc.offset, ptr)
			}
			second := b.NewValue(ssa.OpLoad, ssa.TI64, other, m)
			sum := b.NewValue(ssa.OpAdd, ssa.TI64, first, second)
			b.Kind = ssa.BlockRet
			b.SetControl(m, sum)
			f.Sym = p.Sym(f.Name, ssa.SymFunc)
			p.Funcs = append(p.Funcs, f)
			GenFrees(p)
			GenCheck(f)
			if err := f.Verify(); err != nil {
				t.Fatal(err)
			}
			checks := 0
			for _, block := range f.Blocks {
				for _, v := range block.Values {
					if v.Op == ssa.OpCall && (v.Aux == f.GenLive || v.Aux == f.GenRange) {
						checks++
					}
				}
			}
			if checks != tc.want {
				t.Fatalf("want %d checks, got %d:\n%s", tc.want, checks, f)
			}
		})
	}
}

// Directory checks must not inspect payload prefixes, and a compiler
// inferred custom Free must invalidate a previously cached check.
func TestGenerationDirectoryFreeBarrier(t *testing.T) {
	p := ssa.NewProgram()
	f := ssa.NewFunc("caller")
	f.Params, f.Results = []*ssa.Type{ssa.TPtr}, []*ssa.Type{ssa.TI64}
	f.Pragmas = ssa.PragmaGenCheck
	f.GenLive = p.Sym("pluto/rt.GenRegisteredLive", ssa.SymFunc)
	f.GenRange = p.Sym("pluto/rt.GenRegisteredRangeLive", ssa.SymFunc)
	f.GenPanic = p.Sym("pluto/rt.panicStale", ssa.SymFunc)
	f.GenRangePanic = p.Sym("pluto/rt.panicStaleRange", ssa.SymFunc)
	b := f.Entry
	m := b.NewValue(ssa.OpInitMem, ssa.TMem)
	ptr := b.NewValueI(ssa.OpArg, ssa.TPtr, 0)
	first := b.NewValue(ssa.OpLoad, ssa.TI64, ptr, m)
	free := p.Sym("pluto/rt.GenAfterCustomFree", ssa.SymFunc)
	c := b.NewValueA(ssa.OpCall, ssa.TupleOf(ssa.TMem), free, ptr, m)
	c.Call = &ssa.CallInfo{ABI: ssa.ABIInternal}
	m = b.NewValueI(ssa.OpSelectN, ssa.TMem, 0, c)
	second := b.NewValue(ssa.OpLoad, ssa.TI64, ptr, m)
	sum := b.NewValue(ssa.OpAdd, ssa.TI64, first, second)
	b.Kind = ssa.BlockRet
	b.SetControl(m, sum)
	f.Sym = p.Sym(f.Name, ssa.SymFunc)
	p.Funcs = append(p.Funcs, f)
	GenFrees(p)
	GenCheck(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	lookups, loads := 0, 0
	for _, block := range f.Blocks {
		for _, v := range block.Values {
			if v.Op == ssa.OpCall && (v.Aux == f.GenLive || v.Aux == f.GenRange) {
				lookups++
			}
			if v.Op == ssa.OpLoad {
				loads++
			}
		}
	}
	if lookups != 2 || loads != 2 {
		t.Fatalf("want two lookups and only two payload loads, got %d and %d:\n%s", lookups, loads, f)
	}
}
