// Translated by go2pu from internal/ssa/progcopy.go at the go-cutover tag; edited by hand since.

package ssa

import (
	"pluto/strings"
	"selfhost/compat"
	"selfhost/pu"
)

// A ProgramCopier moves a whole program into fresh memory: its symbols,
// data and functions. The self-hosted compiler lowers the program in
// scratch memory and copies the result out this way, so that what
// lowering allocated and dropped goes when the step ends. Program copies
// the symbols, the data and each function's header, and Func each
// function's code, so that the caller can give each function's code
// memory of its own. NewProgramCopier makes the copier's tables, which
// the copy only reads and fills: they may come from the scratch memory.
type ProgramCopier struct {
	old     *Program
	syms    []*Sym       // the program's symbols
	index   map[*Sym]int // each symbol's place in syms
	newSyms []*Sym       // their copies, made by Program
	copiers []*Copier    // one for each function
	out     *Program
	// The copies of the intrinsic names, made by Program in the
	// program's memory: a function's own memory is freed when a pass
	// replaces its code, and the copy a pass makes shares the Aux.
	intrinsics map[string]*Intrinsic
}

// NewProgramCopier returns a copier for p as it is now.
func NewProgramCopier(p *Program) *ProgramCopier {
	pc := pu.New(ProgramCopier{old: p, syms: p.Syms(), index: map[*Sym]int{}})
	for i, s := range pc.syms {
		pc.index[s] = i
	}
	pc.newSyms = compat.Make[*Sym](len(pc.syms))
	pc.copiers = compat.Make[*Copier](len(p.Funcs))
	for i, f := range p.Funcs {
		c := NewCopier(f)
		c.prog = pc
		pc.copiers[i] = c
	}
	return pc
}

// Sym returns the copy of s, which Program made, or s itself when the
// copier copies a single function (pc is nil).
func (pc *ProgramCopier) Sym(s *Sym) *Sym {
	if pc == nil || s == nil {
		return s
	}
	i, ok := pc.index[s]
	if !ok || pc.syms[i] != s {
		panic("ssa: copying a symbol the program does not hold")
	}
	return pc.newSyms[i]
}

// Program returns the copy of the program, in the current allocator's
// memory: its symbols, its data and each function's header. A function's
// code is still the original's until Func copies it.
func (pc *ProgramCopier) Program() *Program {
	old := pc.old
	out := pu.New(Program{syms: map[string]*Sym{}})
	for i, s := range pc.syms {
		n := pu.New(Sym{Name: strings.Clone(s.Name), Kind: s.Kind, PerCPU: s.PerCPU})
		pc.newSyms[i] = n
		out.syms[n.Name] = n
	}
	out.Data = compat.Make[*Data](len(old.Data))
	for i, d := range old.Data {
		n := pu.New(Data{Sym: pc.Sym(d.Sym), Size: d.Size, Align: d.Align, ReadOnly: d.ReadOnly,
			Section: strings.Clone(d.Section), Local: d.Local})
		if d.Bytes != nil {
			n.Bytes = compat.Make[byte](len(d.Bytes))
			copy(n.Bytes, d.Bytes)
		}
		if d.Relocs != nil {
			n.Relocs = compat.Make[DataReloc](len(d.Relocs))
			for j, r := range d.Relocs {
				n.Relocs[j] = DataReloc{Off: r.Off, Sym: pc.Sym(r.Sym), Addend: r.Addend}
			}
		}
		out.Data[i] = n
	}
	out.Funcs = compat.Make[*Func](len(old.Funcs))
	for i, f := range old.Funcs {
		if f.GenNoFree != nil || f.GenNoReset != nil {
			panic("ssa: copying a program after opt.GenFrees")
		}
		n := pu.New(Func{})
		*n = *f
		n.Name, n.Section, n.Pkg = strings.Clone(f.Name), strings.Clone(f.Section), strings.Clone(f.Pkg)
		n.Sym = pc.Sym(f.Sym)
		if f.Exports != nil {
			n.Exports = compat.Make[string](len(f.Exports))
			for j, e := range f.Exports {
				n.Exports[j] = strings.Clone(e)
			}
		}
		n.GenPanic, n.GenRangePanic = pc.Sym(f.GenPanic), pc.Sym(f.GenRangePanic)
		n.GenLive, n.GenRange = pc.Sym(f.GenLive), pc.Sym(f.GenRange)
		n.DeferRunner = pc.Sym(f.DeferRunner)
		out.Funcs[i] = n
	}
	pc.intrinsics = map[string]*Intrinsic{}
	for _, f := range old.Funcs {
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if aux, ok := v.Aux.(*Intrinsic); ok && pc.intrinsics[string(*aux)] == nil {
					pc.intrinsics[string(*aux)] = NewIntrinsic(strings.Clone(string(*aux)))
				}
			}
		}
	}
	pc.out = out
	return out
}

// Intrinsic returns the copy of the intrinsic name aux, which Program made.
func (pc *ProgramCopier) Intrinsic(aux *Intrinsic) *Intrinsic {
	n := pc.intrinsics[string(*aux)]
	if n == nil {
		panic("ssa: copying an intrinsic the program does not hold")
	}
	return n
}

// Func copies the code of the program's i'th function into the current
// allocator's memory, in place of the original's in Program's copy.
func (pc *ProgramCopier) Func(i int) {
	pc.copiers[i].Copy(pc.out.Funcs[i])
}
