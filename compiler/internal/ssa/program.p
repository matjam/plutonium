// Translated by go2pu from internal/ssa/program.go at the go-cutover tag; edited by hand since.

package ssa

import (
	"compiler/compat"
	"compiler/internal/syntax/token"
	"compiler/pu"
	"pluto/sort"
)

// A Program is everything the backend emits: functions and data.
type Program struct {
	Funcs []*Func
	Data  []*Data
	syms  map[string]*Sym

	// Stack is what the backend needs to compute the stacks of tasks
	// (language spec section 19.1, item 1), or nil when lowering leaves
	// every task's stack to the runtime.
	Stack *StackInfo
}

// StackInfo describes the task starts whose stack the backend bounds, and
// the indirect calls of the program as lowering resolved them (language
// spec section 20.10).
type StackInfo struct {
	Roots []StackRoot
	// Indirect maps the name of each function lowered with indirect calls
	// to the functions those calls may reach. Inlining moves a call into
	// another function: the backend takes the indirect calls of a function
	// to reach what Indirect gives for it and for each function inlined
	// into it (Func.Inlines).
	Indirect map[string][]string
	// Open holds the functions with an indirect call whose callees are not
	// known: such a call may reach any function.
	Open map[string]bool
	// Off, if set, says why no task gets a bound: the roots are recorded
	// for the report only, without Bound words.
	Off string
}

// A StackRoot is a task start: the task statement at Pos starts the
// function Thunk, and loads its stack bound from Bound, an 8-byte
// read-only word the backend fills in (0 when it finds no bound), or nil
// when StackInfo.Off is set.
type StackRoot struct {
	Thunk string
	Bound *Sym
	Pos   token.Pos
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
