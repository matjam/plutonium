package lower

// The lowering oracle's view of the lowered program (plan
// docs/plans/2026-10-20-escape-redesign.md, section 3.1, "The lowering
// oracle"): every call the generated code makes, at which source position,
// and to which functions, so that an escape analysis can be checked to have
// seen each of them. It reads what lowering records anyway (callOrigins,
// sites, symFuncs, bodies, funcValues, itabs) and changes nothing.
//
// This file is not translated by tools/go2pu (its untranslated map): it is
// test machinery of the Go compiler.

import (
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// CallFacts is the lowered program as the lowering oracle sees it: the
// functions as lowered, before any optimisation or inlining, and what each
// call in them reaches.
type CallFacts struct {
	l      *Lowerer
	g      *callGraph
	lits   map[*ast.BlockStmt]*ast.FuncLit
	ranges map[token.Pos]*ast.RangeStmt
	decls  map[*ast.BlockStmt]*ast.FuncDecl
	expand map[*ssa.Sym][]*ssa.Sym
	reach  map[*ssa.Sym]bool // reachableDescs
}

// LowerFacts lowers prog as Lower does and returns the oracle's view of the
// result. The view is returned even when lowering reports errors, for the
// functions lowered before them; err is lowering's error.
func LowerFacts(cfg *Config, prog *load.Program) (*CallFacts, error) {
	var l *Lowerer
	c := *cfg
	c.Keep = func(x *Lowerer) { l = x }
	_, err := Lower(&c, prog)
	if l == nil {
		return nil, err
	}
	cf := &CallFacts{
		l:      l,
		lits:   map[*ast.BlockStmt]*ast.FuncLit{},
		ranges: map[token.Pos]*ast.RangeStmt{},
		decls:  map[*ast.BlockStmt]*ast.FuncDecl{},
		expand: map[*ssa.Sym][]*ssa.Sym{},
	}
	for _, p := range prog.Packages {
		for _, f := range p.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncLit:
					cf.lits[n.Body] = n
				case *ast.FuncDecl:
					if n.Body != nil {
						cf.decls[n.Body] = n
					}
				case *ast.RangeStmt:
					cf.ranges[n.Pos()] = n
				}
				return true
			})
		}
	}
	g := &callGraph{l: l, funcs: l.funcsBySym(), edges: map[*ssa.Sym][]hop{}, ifaceTargets: map[string][]*ssa.Sym{}}
	for sym := range l.funcValues {
		g.valueFuncs = append(g.valueFuncs, sym)
	}
	sort.Slice(g.valueFuncs, func(i, j int) bool { return g.valueFuncs[i].Name < g.valueFuncs[j].Name })
	cf.g = g
	return cf, err
}

// Funcs returns the lowered functions, sorted by name.
func (cf *CallFacts) Funcs() []*ssa.Func {
	out := append([]*ssa.Func(nil), cf.l.out.Funcs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Func returns the lowered function of a symbol, or nil.
func (cf *CallFacts) Func(sym *ssa.Sym) *ssa.Func { return cf.g.funcs[sym] }

// Origin says where a lowered function comes from. At most one of Obj, Decl,
// Lit and Range is set for source functions; a function with none of them is
// synthesized by the lowerer (a wrapper, adapter, thunk, drop glue or
// generated method), or Extern.
type Origin struct {
	Obj    *types2.Func   // a declared function or method (origin of an instance)
	Decl   *ast.FuncDecl  // its declaration; set alone for a package's init functions
	Lit    *ast.FuncLit   // a function literal
	Range  *ast.RangeStmt // the body of a range over a function (section 12.11)
	Extern bool           // a function without a body: assembly, C, //pu:extern
}

// Source reports whether the origin is source code: a declaration, literal
// or range body, or a body-less declaration.
func (o Origin) Source() bool {
	return o.Obj != nil || o.Decl != nil || o.Lit != nil || o.Range != nil
}

// Origin returns where the function of sym comes from.
func (cf *CallFacts) Origin(sym *ssa.Sym) Origin {
	l := cf.l
	var o Origin
	if fn := l.symFuncs[sym]; fn != nil {
		o.Obj = fn.Origin()
		if d := l.decls[o.Obj]; d != nil {
			o.Decl = d.decl
			if d.decl.Body == nil {
				o.Extern = true
			}
		} else if sym.Kind == ssa.SymExtern {
			o.Extern = true
		}
	}
	if fb, ok := l.bodies[sym]; ok {
		if lit := cf.lits[fb.body]; lit != nil {
			return Origin{Lit: lit}
		}
		if o.Decl == nil {
			o.Decl = cf.decls[fb.body]
		}
		return o
	}
	if o.Obj != nil {
		if f := cf.g.funcs[sym]; f != nil && o.Decl != nil && o.Decl.Body != nil {
			// A declared function lowered by a synthesized job (a hook
			// body chosen for the program): its calls are its own.
			return o
		}
		if o.Extern || cf.g.funcs[sym] == nil {
			return o
		}
	}
	if f := cf.g.funcs[sym]; f != nil && strings.Contains(sym.Name, "-range") {
		if st := cf.ranges[f.Pos]; st != nil {
			return Origin{Range: st}
		}
	}
	return Origin{}
}

// CallKind says how a lowered call finds its callee.
type CallKind uint8

const (
	CallStatic    CallKind = iota + 1 // OpCall of Static, or OpCallInd of a known function address
	CallIface                         // through an itab: Iface's method Method
	CallFuncVal                       // through a func value of signature FSig
	CallDropAdapt                     // a dynamic interface's descriptor drop adapter
	CallUnknown                       // an indirect call lowering recorded nothing about
)

// Call is one call in a lowered function.
type Call struct {
	Value    *ssa.Value
	Pos      token.Pos // the source position lowering gave the call
	Implicit bool      // a call the compiler made up, into the runtime
	Kind     CallKind
	Static   *ssa.Sym
	Iface    types2.Type
	Method   int
	FSig     *types2.Signature
	Targets  []*ssa.Sym // every function the call may reach, as lowered
	// Unconverted are the targets of a call through an itab whose dynamic
	// type the generated code never puts into an interface, directly or by
	// walking a type descriptor (pluto/fmt): lowering emits itabs for every
	// type with a descriptor and every interface asserted, so these are
	// methods no run of the program can reach there.
	Unconverted map[*ssa.Sym]bool
}

// Calls returns the calls f makes, in block order.
func (cf *CallFacts) Calls(f *ssa.Func) []Call {
	var out []Call
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			switch v.Op {
			case ssa.OpCall, ssa.OpCallInd:
			default:
				continue
			}
			c := Call{Value: v, Pos: cf.g.callPos(v), Implicit: v.Call != nil && v.Call.Implicit}
			switch {
			case v.Op == ssa.OpCall:
				callee, _ := v.Aux.(*ssa.Sym)
				if callee == nil {
					c.Kind = CallUnknown
					break
				}
				if exported := cf.l.cnames[callee.Name]; callee.Kind == ssa.SymExtern && exported != nil {
					callee = exported
				}
				c.Kind, c.Static, c.Targets = CallStatic, callee, []*ssa.Sym{callee}
			case v.Args[0].Op == ssa.OpFuncAddr:
				callee := v.Args[0].Aux.(*ssa.Sym)
				c.Kind, c.Static, c.Targets = CallStatic, callee, []*ssa.Sym{callee}
			default:
				site := cf.l.sites[v]
				switch {
				case site == nil:
					c.Kind = CallUnknown
				case site.drop:
					c.Kind, c.Iface = CallDropAdapt, site.dropType
				case site.iface != nil:
					c.Kind, c.Iface, c.Method = CallIface, site.iface, site.method
				default:
					c.Kind, c.FSig = CallFuncVal, site.fsig
				}
				if site != nil {
					c.Targets = cf.g.targets(site)
				}
				if site != nil && site.iface != nil {
					c.Unconverted = cf.unconverted(site)
				}
			}
			out = append(out, c)
		}
	}
	return out
}

// MethodName returns the name of method index i of interface type T, or "".
func MethodName(T types2.Type, i int) string {
	it, ok := T.Underlying().(*types2.Interface)
	if !ok || i < 0 || i >= it.NumMethods() {
		return ""
	}
	return it.Method(i).Name()
}

// SourceTargets returns the source functions a call to sym reaches: sym
// itself if it is source (or not lowered at all), otherwise, for a function
// the lowerer synthesized, the source functions its own calls reach, through
// any number of synthesized functions.
func (cf *CallFacts) SourceTargets(sym *ssa.Sym) []*ssa.Sym {
	if out, ok := cf.expand[sym]; ok {
		return out
	}
	out := cf.sourceTargets(sym, map[*ssa.Sym]bool{})
	cf.expand[sym] = out
	return out
}

func (cf *CallFacts) sourceTargets(sym *ssa.Sym, active map[*ssa.Sym]bool) []*ssa.Sym {
	f := cf.g.funcs[sym]
	if f == nil || cf.Origin(sym).Source() {
		return []*ssa.Sym{sym}
	}
	if active[sym] {
		return nil
	}
	active[sym] = true
	seen := map[*ssa.Sym]bool{}
	var out []*ssa.Sym
	for _, c := range cf.Calls(f) {
		for _, t := range c.Targets {
			for _, s := range cf.sourceTargets(t, active) {
				if !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
		}
	}
	delete(active, sym)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Position returns the source position of pos.
func (cf *CallFacts) Position(pos token.Pos) token.Position { return cf.l.fset.Position(pos) }

// unconverted returns the targets of the interface call site whose itab's
// dynamic type is not reachable (reachableDescs).
func (cf *CallFacts) unconverted(site *callSite) map[*ssa.Sym]bool {
	reach := cf.reachableDescs()
	var out map[*ssa.Sym]bool
	for _, it := range cf.l.types.itabs {
		if !types2.Identical(it.I, site.iface) || site.method >= len(it.methods) {
			continue
		}
		d := cf.l.types.descs[typeString(it.T)]
		if d != nil && reach[d.sym] {
			continue
		}
		if out == nil {
			out = map[*ssa.Sym]bool{}
		}
		out[it.methods[site.method]] = true
	}
	return out
}

// reachableDescs returns the type descriptors of the types the generated
// code may put into an interface: those whose descriptor or itab address it
// uses other than in a comparison (an assertion compares, a conversion
// stores), and every type reachable from those through descriptors (element,
// field and pointer-to descriptors), which a descriptor walk such as
// pluto/fmt's may make interfaces of.
func (cf *CallFacts) reachableDescs() map[*ssa.Sym]bool {
	if cf.reach != nil {
		return cf.reach
	}
	tt := cf.l.types
	descOf := map[*ssa.Sym]*descInfo{}
	for _, d := range tt.descs {
		descOf[d.sym] = d
	}
	itabOf := map[*ssa.Sym]*itabInfo{}
	for _, it := range tt.itabs {
		itabOf[it.sym] = it
	}
	reach := map[*ssa.Sym]bool{}
	var work []*descInfo
	mark := func(d *descInfo) {
		if d != nil && !reach[d.sym] {
			reach[d.sym] = true
			work = append(work, d)
		}
	}
	for _, f := range cf.l.out.Funcs {
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op == ssa.OpEq || v.Op == ssa.OpNe {
					continue
				}
				for _, a := range v.Args {
					if a.Op != ssa.OpSymAddr {
						continue
					}
					sym, _ := a.Aux.(*ssa.Sym)
					if d := descOf[sym]; d != nil {
						mark(d)
					} else if it := itabOf[sym]; it != nil {
						mark(tt.descs[typeString(it.T)])
					}
				}
			}
		}
	}
	for len(work) > 0 {
		d := work[len(work)-1]
		work = work[:len(work)-1]
		mark(descOf[d.elem])
		mark(descOf[d.ptrTo])
		for _, fd := range d.fields {
			mark(descOf[fd.typ])
		}
	}
	cf.reach = reach
	return reach
}
