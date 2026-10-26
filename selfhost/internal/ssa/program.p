// Translated by go2pu from internal/ssa/program.go at the go-cutover tag; edited by hand since.

package ssa

import (
	"pluto/sort"
	"selfhost/compat"
	"selfhost/pu"
)

// A Program is everything the backend emits: functions and data.
type Program struct {
	Funcs []*Func
	Data  []*Data
	syms  map[string]*Sym
}

// NewProgram returns an empty program.
func NewProgram() *Program {
	return pu.New(Program{syms: map[string]*Sym{}})
}

// Sym returns the symbol with the given name, creating it if needed.
func (p *Program) Sym(name string, kind SymKind) *Sym {
	if s := p.syms[name]; s != nil {
		// A symbol first seen as external becomes defined when its
		// definition appears.
		if s.Kind == SymExtern && kind != SymExtern {
			s.Kind = kind
		}
		return s
	}
	s := pu.New(Sym{Name: name, Kind: kind})
	p.syms[name] = s
	return s
}

// LookupSym returns the named symbol or nil.
func (p *Program) LookupSym(name string) *Sym { return p.syms[name] }

// Syms returns all symbols sorted by name.
func (p *Program) Syms() []*Sym {
	out := compat.MakeCap[*Sym](0, len(p.syms))
	for _, s := range p.syms {
		out = compat.Append(out, s)
	}
	sort.SortFunc(out, func(a, b *Sym) bool { return a.Name < b.Name })
	return out
}

// A Data is a statically allocated object.
type Data struct {
	Sym      *Sym
	Size     int64
	Align    int64
	Bytes    []byte // initial contents (len <= Size); nil means all zero
	Relocs   []DataReloc
	ReadOnly bool
	Section  string // explicit section name, or ""
	Local    bool   // not visible outside the object file
}

// A DataReloc places the address of Sym plus Addend at Off.
type DataReloc struct {
	Off    int64
	Sym    *Sym
	Addend int64
}
