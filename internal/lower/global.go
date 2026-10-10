package lower

import (
	"encoding/binary"
	"fmt"
	"go/constant"
	"math"
	"slices"
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
		sections, percpu := globalSections(p)
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
			if _, ok := percpu[v]; ok {
				l.perCPUVar(v, d, percpu[v])
			}
			// Static initialisation from a constant, or from a literal
			// array or struct made of constants.
			hasInit := false
			for _, init := range p.Info.InitOrder {
				hasInit = hasInit || slices.Contains(init.Lhs, v)
				if len(init.Lhs) == 1 && init.Lhs[0] == v {
					if tv := p.Info.Types[init.Rhs]; tv.Value != nil {
						if l.staticConst(d, tv.Value, T) {
							static[v] = true
						}
					} else if _, ok := ast.Unparen(init.Rhs).(*ast.CompositeLit); ok {
						d.Bytes = make([]byte, d.Size)
						if l.staticValue(p, d, init.Rhs, T, 0) {
							static[v] = true
						} else {
							d.Bytes, d.Relocs = nil, nil
						}
					}
				}
			}
			if sym.PerCPU && hasInit && !static[v] {
				l.errorf(percpu[v], "per-CPU variable %s must have a constant initialiser: package initialisation runs before any processor has a per-CPU block", v.Name())
			}
		}
		l.queueInit(p, static)
	}
}

// staticObject allocates an anonymous static object of type T.
func (l *Lowerer) staticObject(T types2.Type) *ssa.Sym {
	return l.staticBytes(l.sizeof(T), l.alignof(T))
}

// staticBytes allocates an anonymous static object of the given size.
func (l *Lowerer) staticBytes(size, align int64) *ssa.Sym {
	sym := l.out.Sym(fmt.Sprintf(".static.%d", l.statics), ssa.SymData)
	l.statics++
	l.out.Data = append(l.out.Data, &ssa.Data{Sym: sym, Size: max(size, 1), Align: align, Local: true})
	return sym
}

// perCPUSection holds //pu:percpu variables; the kernel's linker script
// links it at address 0.
const perCPUSection = ".percpu"

// perCPUSelf is the word at offset 0 of every per-CPU block, holding the
// block's own address, which taking a per-CPU variable's address needs.
const perCPUSelf = "pluto.percpu.self"

// perCPUVar makes package variable v, with data d, a per-CPU variable.
func (l *Lowerer) perCPUVar(v *types2.Var, d *ssa.Data, pos token.Pos) {
	switch {
	case !l.cfg.Kernel:
		l.errorf(pos, "//pu:percpu variable %s needs --mode kernel", v.Name())
		return
	case d.Section != "":
		l.errorf(pos, "//pu:percpu variable %s cannot also have //pu:section", v.Name())
		return
	}
	d.Sym.PerCPU = true
	d.Section = perCPUSection
	if l.perCPUSelfSym == nil {
		// The self word comes first: emitObject places it at the start
		// of .percpu, so it is at offset 0 when this object is linked
		// first (as pluto build does).
		self := l.out.Sym(perCPUSelf, ssa.SymData)
		self.PerCPU = true
		l.perCPUSelfSym = self
		l.out.Data = append(l.out.Data, &ssa.Data{Sym: self, Size: 8, Align: 8, Section: perCPUSection})
	}
}

// absAddr turns p, if it addresses a per-CPU variable (OpSymAddr of a
// per-CPU symbol, under constant or indexed offsets), into an ordinary
// pointer into the current processor's copy: the block's self word, read
// from %gs:0 now, plus the variable's offset. Lowering calls it wherever an
// address becomes a value (&v, slicing, a pointer receiver), so the
// pointer keeps referring to this processor's copy after GS changes, as
// abi.md section 10.6 says; direct accesses keep the GS-relative form.
func (s *fnState) absAddr(p *ssa.Value) *ssa.Value {
	switch p.Op {
	case ssa.OpSymAddr:
		sym := p.Aux.(*ssa.Sym)
		if !sym.PerCPU {
			return p
		}
		self := s.load(ssa.TPtr, s.symAddr(s.l.perCPUSelfSym))
		off := s.f.Entry.NewValue(ssa.OpPerCPUOffset, ssa.TI64)
		off.Aux = sym
		s.hoistEntry(off)
		return s.newValue(ssa.OpAddPtr, ssa.TPtr, self, off)
	case ssa.OpOffPtr:
		if base := s.absAddr(p.Args[0]); base != p.Args[0] {
			return s.offPtr(base, p.AuxInt)
		}
	case ssa.OpAddPtr:
		if base := s.absAddr(p.Args[0]); base != p.Args[0] {
			return s.newValue(ssa.OpAddPtr, ssa.TPtr, base, p.Args[1])
		}
	}
	return p
}

// globalSections reads //pu:section and //pu:percpu directives on
// package-level variables. percpu maps each per-CPU variable to the
// position of its declaration.
func globalSections(p *load.Package) (map[*types2.Var]string, map[*types2.Var]token.Pos) {
	out := map[*types2.Var]string{}
	percpu := map[*types2.Var]token.Pos{}
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
					name, isSection := strings.CutPrefix(dir, "section ")
					if !isSection && dir != "percpu" {
						continue
					}
					for _, n := range vs.Names {
						v, ok := p.Info.Defs[n].(*types2.Var)
						switch {
						case !ok:
						case isSection:
							out[v] = strings.TrimSpace(name)
						default:
							percpu[v] = n.Pos()
						}
					}
				}
			}
		}
	}
	return out, percpu
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

// staticValue writes the value of e, of type T, into d.Bytes at offset
// off, reporting whether e is a constant or a literal array or struct
// whose elements are, recursively, such values. Elements a literal leaves
// out stay zero.
func (l *Lowerer) staticValue(p *load.Package, d *ssa.Data, e ast.Expr, T types2.Type, off int64) bool {
	T = unwrap(T)
	if tv := p.Info.Types[e]; tv.Value != nil {
		var c ssa.Data
		if !l.staticConst(&c, tv.Value, T) {
			return false
		}
		copy(d.Bytes[off:], c.Bytes)
		for _, r := range c.Relocs {
			r.Off += off
			d.Relocs = append(d.Relocs, r)
		}
		return true
	}
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	if !ok {
		return false
	}
	switch u := T.Underlying().(type) {
	case *types2.Array:
		et := u.Elem()
		size := l.sizeof(et)
		var i int64
		for _, elt := range lit.Elts {
			val := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if i, ok = constIndex(p.Info, kv.Key); !ok {
					return false
				}
				val = kv.Value
			}
			if !l.staticValue(p, d, val, et, off+i*size) {
				return false
			}
			i++
		}
		return true
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := l.sizes.Offsetsof(fields)
		for i, elt := range lit.Elts {
			idx, val := i, elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				key, _ := kv.Key.(*ast.Ident)
				idx = -1
				for j, f := range fields {
					if key != nil && f.Name() == key.Name {
						idx = j
					}
				}
				if idx < 0 {
					return false
				}
				val = kv.Value
			}
			if !l.staticValue(p, d, val, fields[idx].Type(), off+offs[idx]) {
				return false
			}
		}
		return true
	}
	return false
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
			if len(init.Lhs) == 1 && init.Lhs[0].Name() == "_" && p.Info.Types[init.Rhs].Value != nil {
				// var _ = constant has no effect.
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
	// pluto.mainstack returns the size of the stack main runs on, from
	// main's //pu:stack directive, or 0 for the stack the system gave the
	// process. The hosted runtime reserves the stack before calling
	// pluto.main.
	stack := l.mainStackSize(mainFn)
	uptr := types2.Typ[types2.Uintptr]
	sizeSig := types2.NewSignatureType(nil, nil, nil, nil,
		types2.NewTuple(types2.NewParam(token.NoPos, nil, "", uptr)), false)
	l.queue = append(l.queue, &funcJob{sym: l.out.Sym("pluto.mainstack", ssa.SymFunc), sig: sizeSig, build: func(s *fnState) {
		s.returnValues([]value{scalarValue(s.constInt(ssa.TI64, int64(stack)), uptr)})
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
