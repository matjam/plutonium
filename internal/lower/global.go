package lower

import (
	"encoding/binary"
	"fmt"
	"go/constant"
	"math"
	"strings"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

var constOne = constant.MakeInt64(1)

// globalSym returns the symbol of a package-level variable.
func (l *Lowerer) globalSym(v *types2.Var) *ssa.Sym {
	if sym := l.globals[v]; sym != nil {
		return sym
	}
	sym := l.out.Sym(pkgPath(v.Pkg())+"."+v.Name(), ssa.SymData)
	l.globals[v] = sym
	return sym
}

// lowerGlobals allocates every package-level variable, initialising it
// statically when its initializer is a constant, and queues each package's
// init function for the rest.
func (l *Lowerer) lowerGlobals() {
	for _, p := range l.prog.Packages {
		static := map[*types2.Var]bool{}
		sections := globalSections(p)
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			v, ok := scope.Lookup(name).(*types2.Var)
			if !ok {
				continue
			}
			sym := l.globalSym(v)
			T := v.Type()
			d := &ssa.Data{Sym: sym, Size: l.sizeof(T), Align: l.alignof(T), Section: sections[v]}
			if d.Size == 0 {
				d.Size = 1
			}
			l.out.Data = append(l.out.Data, d)
			// Static initialisation from a constant.
			for _, init := range p.Info.InitOrder {
				if len(init.Lhs) == 1 && init.Lhs[0] == v {
					if tv := p.Info.Types[init.Rhs]; tv.Value != nil {
						if l.staticConst(d, tv.Value, T) {
							static[v] = true
						}
					}
				}
			}
		}
		l.queueInit(p, static)
	}
}

// staticObject allocates an anonymous static object of type T.
func (l *Lowerer) staticObject(T types2.Type) *ssa.Sym {
	sym := l.out.Sym(fmt.Sprintf(".static.%d", l.statics), ssa.SymData)
	l.statics++
	l.out.Data = append(l.out.Data, &ssa.Data{Sym: sym, Size: max(l.sizeof(T), 1), Align: l.alignof(T), Local: true})
	return sym
}

// globalSections reads //pu:section directives on package-level variables.
func globalSections(p *load.Package) map[*types2.Var]string {
	out := map[*types2.Var]string{}
	for _, f := range p.Files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				doc := vs.Doc
				if doc == nil {
					doc = gd.Doc
				}
				for _, dir := range directives(doc) {
					if name, ok := strings.CutPrefix(dir, "section "); ok {
						for _, n := range vs.Names {
							if v, ok := p.Info.Defs[n].(*types2.Var); ok {
								out[v] = strings.TrimSpace(name)
							}
						}
					}
				}
			}
		}
	}
	return out
}

// staticConst fills d with constant c of type T, if T is a scalar or string.
func (l *Lowerer) staticConst(d *ssa.Data, c constant.Value, T types2.Type) bool {
	T = unwrap(T)
	if isString(T) {
		str := constant.StringVal(c)
		buf := make([]byte, 16)
		binary.LittleEndian.PutUint64(buf[8:], uint64(len(str)))
		d.Bytes = buf
		if str != "" {
			d.Relocs = append(d.Relocs, ssa.DataReloc{Off: 0, Sym: l.stringData(str)})
		}
		return true
	}
	t := l.scalarType(T)
	if t == nil || t.IsPtr() {
		return false
	}
	var bits uint64
	switch {
	case c.Kind() == constant.Bool:
		if constant.BoolVal(c) {
			bits = 1
		}
	case t.IsFloat():
		f, _ := constant.Float64Val(c)
		if t.Size == 4 {
			bits = uint64(math.Float32bits(float32(f)))
		} else {
			bits = math.Float64bits(f)
		}
	default:
		if i, ok := constant.Int64Val(constant.ToInt(c)); ok {
			bits = uint64(i)
		} else {
			bits, _ = constant.Uint64Val(constant.ToInt(c))
		}
	}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, bits)
	d.Bytes = buf[:t.Size]
	return true
}

// initSymName names a package's synthesized initialisation function. User
// init functions are named path.init.0, path.init.1, ... so they cannot
// clash with it or with each other.
func (l *Lowerer) initSymName(p *load.Package) string { return p.Path + ".init·pkg" }

// queueInit queues the package's init function: dynamic variable
// initialisation in dependency order, then the package's init functions.
func (l *Lowerer) queueInit(p *load.Package, static map[*types2.Var]bool) {
	sym := l.out.Sym(l.initSymName(p), ssa.SymFunc)
	sig := types2.NewSignatureType(nil, nil, nil, nil, nil, false)
	var inits []*ssa.Sym
	for _, f := range p.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "init" && fd.Recv == nil && fd.Body != nil {
				isym := l.out.Sym(fmt.Sprintf("%s.init.%d", p.Path, len(inits)), ssa.SymFunc)
				inits = append(inits, isym)
				l.queue = append(l.queue, &funcJob{sym: isym, pkg: p, decl: fd, sig: sig})
			}
		}
	}
	l.queue = append(l.queue, &funcJob{sym: sym, pkg: p, sig: sig, build: func(s *fnState) {
		s.staticLits = true
		for _, init := range p.Info.InitOrder {
			if len(init.Lhs) == 1 && static[init.Lhs[0]] {
				continue
			}
			if len(init.Lhs) == 1 {
				v := init.Lhs[0]
				val := s.exprAs(init.Rhs, v.Type())
				if v.Name() != "_" {
					s.storeTo(s.symAddr(l.globalSym(v)), val, v.Type())
				}
				continue
			}
			vals := s.multiValue(init.Rhs, len(init.Lhs))
			for i, v := range init.Lhs {
				if v.Name() != "_" {
					s.storeTo(s.symAddr(l.globalSym(v)), s.assignConvert(vals[i], v.Type(), init.Rhs), v.Type())
				}
			}
		}
		for _, isym := range inits {
			s.emitCall(callee{sym: isym}, sig, nil, nil, token.NoPos)
		}
		s.emitReturn(token.NoPos)
	}})
}

// lowerEntry synthesizes pluto.main: run every package's initialisation in
// dependency order, then main.main, and return the exit status.
func (l *Lowerer) lowerEntry() {
	mainFn, _ := l.prog.Main.Types.Scope().Lookup("main").(*types2.Func)
	if mainFn == nil {
		l.errs = append(l.errs, &Error{Msg: "package main has no main function"})
		return
	}
	i64 := types2.Typ[types2.Int64]
	sig := types2.NewSignatureType(nil, nil, nil, nil,
		types2.NewTuple(types2.NewParam(token.NoPos, nil, "", i64)), false)
	noArgs := types2.NewSignatureType(nil, nil, nil, nil, nil, false)
	// pluto.itabtable returns the address of the itab table, which the
	// runtime searches for interface-to-interface conversions.
	ptrSig := types2.NewSignatureType(nil, nil, nil, nil,
		types2.NewTuple(types2.NewParam(token.NoPos, nil, "", types2.Typ[types2.UnsafePointer])), false)
	l.queue = append(l.queue, &funcJob{sym: l.out.Sym("pluto.itabtable", ssa.SymFunc), sig: ptrSig, build: func(s *fnState) {
		addr := s.symAddr(l.out.Sym("pluto.itabs", ssa.SymData))
		s.returnValues([]value{scalarValue(addr, types2.Typ[types2.UnsafePointer])})
	}})

	sym := l.out.Sym("pluto.main", ssa.SymFunc)
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		for _, p := range l.prog.Packages {
			s.emitCall(callee{sym: l.out.Sym(l.initSymName(p), ssa.SymFunc)}, noArgs, nil, nil, token.NoPos)
		}
		msig := mainFn.Type().(*types2.Signature)
		res := s.emitCall(callee{sym: l.funcSym(mainFn, nil)}, msig, nil, nil, token.NoPos)
		var status value
		if len(res) == 1 {
			status = scalarValue(s.convertScalar(res[0].v, res[0].T, i64), i64)
		} else {
			status = scalarValue(s.constInt(ssa.TI64, 0), i64)
		}
		s.returnValues([]value{status})
	}})
}
