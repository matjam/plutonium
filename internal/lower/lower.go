// Package lower translates type-checked Plutonium packages into SSA.
//
// Lowering is deliberately simple. Every local variable, parameter and
// aggregate temporary lives in a stack slot and is reached through loads and
// stores; the ssa package's scalar replacement and mem2reg passes turn those
// into SSA values. Only the memory state itself is put into SSA form here,
// with the on-the-fly construction of Braun et al. ("Simple and Efficient
// Construction of Static Single Assignment Form", CC 2013).
//
// Generic functions are instantiated on demand: a call to F[int] queues F's
// body to be lowered with its type parameters replaced by int. Methods are
// instantiated the same way, with the receiver type's arguments followed by
// the method's own when it declares type parameters (v.Map[int] on a
// Vec[string] lowers (*Vec).Map with T=string, U=int).
package lower

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Config controls lowering.
type Config struct {
	Freestanding bool
	// Kernel compiles for ring 0 (--mode kernel): functions not marked
	// //pu:fpu may not use floating point, and //pu:percpu is allowed.
	Kernel bool
	Sizes  types2.Sizes

	// GenCheck checks the generation of heap pointers on every access
	// (prototype, gencheck.go). The standard library must be built with the
	// gencheck tag so that its allocator tags pointers.
	GenCheck bool
	// NoFieldNames leaves the field names out of type descriptors, so %+v
	// prints struct fields without them (pluto build --fieldnames=false).
	NoFieldNames bool
}

// A Lowerer lowers one program.
type Lowerer struct {
	cfg   *Config
	prog  *load.Program
	fset  *token.FileSet
	sizes types2.Sizes
	out   *ssa.Program
	ctxt  *types2.Context

	pkgOf map[*types2.Package]*load.Package
	decls map[*types2.Func]*funcDecl // every function and method declared in source

	funcs          map[string]*ssa.Sym // instantiated functions by symbol name
	queue          []*funcJob
	dropNeeds      map[string]bool // whether destroying a value of a type runs code, by type (drop.go)
	globals        map[*types2.Var]*ssa.Sym
	strs           map[string]*ssa.Sym
	closureRecords map[*ssa.Sym]*ssa.Sym      // static closure record per function used as a value
	exports        map[*ssa.Sym][]string      // extra symbol names from //pu:export
	interruptSyms  map[*ssa.Sym]bool          // //pu:interrupt functions
	cfuncs         map[*ssa.Sym]bool          // //pu:extern symbols, called with the C convention
	statics        int                        // anonymous static objects created
	errorSites     map[*ast.CallExpr]*ssa.Sym // static error of each errors.New call site
	perCPUSelfSym  *ssa.Sym                   // pluto.percpu.self, once a per-CPU variable exists
	genPanic       *ssa.Sym                   // pluto/rt.panicStale, under GenCheck
	types          *typeTable

	// panicFn is the program's panic handler (panic.go).
	panicFn *types2.Func
	// What the interrupt-context check (context.go) needs to know about
	// the lowered program: the declared function behind each symbol, the
	// source and signature of each lowered function, the functions whose
	// values are taken, and what each indirect call can reach.
	symFuncs   map[*ssa.Sym]*types2.Func
	bodies     map[*ssa.Sym]funcBody
	sigs       map[*ssa.Sym]*types2.Signature
	funcValues map[*ssa.Sym]*types2.Signature
	sites      map[*ssa.Value]*callSite

	// instances of types declared in generic functions (typeMap.local)
	localTypes map[localKey]*types2.Named

	errs diag.List
}

// funcDecl locates a function's source.
type funcDecl struct {
	pkg  *load.Package
	decl *ast.FuncDecl
	obj  *types2.Func
}

// A funcJob is a function body waiting to be lowered.
type funcJob struct {
	sym  *ssa.Sym
	pkg  *load.Package
	decl *ast.FuncDecl // nil for closures and synthesized functions
	lit  *ast.FuncLit  // for closures
	sig  *types2.Signature
	tmap *typeMap // type parameter substitution, or nil

	// For closures: the variables captured from the enclosing function, in
	// the order they are stored in the closure record.
	captures []*types2.Var
	parent   *fnState

	build func(s *fnState) // for synthesized functions (init, wrappers)
}

// Lower lowers a loaded program.
func Lower(cfg *Config, prog *load.Program) (_ *ssa.Program, err error) {
	var l *Lowerer
	defer func() {
		// Lowering carries on after an error with placeholder values, which
		// later code may trip over. Such a crash is a consequence of the
		// errors already reported, so report those instead.
		if r := recover(); r != nil {
			if l == nil || len(l.errs) == 0 {
				panic(r)
			}
			err = l.errs
		}
	}()
	l = &Lowerer{
		cfg:            cfg,
		prog:           prog,
		fset:           prog.Fset,
		sizes:          cfg.Sizes,
		out:            ssa.NewProgram(),
		ctxt:           types2.NewContext(),
		pkgOf:          map[*types2.Package]*load.Package{},
		decls:          map[*types2.Func]*funcDecl{},
		funcs:          map[string]*ssa.Sym{},
		dropNeeds:      map[string]bool{},
		globals:        map[*types2.Var]*ssa.Sym{},
		strs:           map[string]*ssa.Sym{},
		closureRecords: map[*ssa.Sym]*ssa.Sym{},
		localTypes:     map[localKey]*types2.Named{},
		errorSites:     map[*ast.CallExpr]*ssa.Sym{},
		exports:        map[*ssa.Sym][]string{},
		interruptSyms:  map[*ssa.Sym]bool{},
		cfuncs:         map[*ssa.Sym]bool{},
		symFuncs:       map[*ssa.Sym]*types2.Func{},
		bodies:         map[*ssa.Sym]funcBody{},
		sigs:           map[*ssa.Sym]*types2.Signature{},
		funcValues:     map[*ssa.Sym]*types2.Signature{},
		sites:          map[*ssa.Value]*callSite{},
	}
	l.types = newTypeTable(l)
	if l.sizes == nil {
		l.sizes = types2.SizesFor("gc", "amd64")
	}
	for _, p := range prog.Packages {
		l.pkgOf[p.Types] = p
		for _, f := range p.Files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					if obj, ok := p.Info.Defs[fd.Name].(*types2.Func); ok {
						l.decls[obj] = &funcDecl{pkg: p, decl: fd, obj: obj}
					}
				}
			}
		}
	}

	l.findPanicHandler()
	l.lowerGlobals()
	l.lowerEntry()
	l.checkInterrupts()
	l.checkCDecls()
	l.lowerExports()
	for {
		for len(l.queue) > 0 && len(l.errs) < 50 {
			job := l.queue[0]
			l.queue = l.queue[1:]
			l.lowerFunc(job)
		}
		if len(l.errs) > 0 || !l.types.finish() {
			break
		}
	}
	l.types.emit()
	if len(l.errs) == 0 {
		l.checkPanicHandler()
		l.checkContexts()
	}
	if len(l.errs) > 0 {
		return nil, l.errs
	}
	l.dropUnusedGlobals()
	sort.Slice(l.out.Funcs, func(i, j int) bool { return l.out.Funcs[i].Name < l.out.Funcs[j].Name })
	return l.out, nil
}

// errorf reports an error in the program at pos. code says what kind of
// error it is; internal/diag explains it.
func (l *Lowerer) errorf(pos token.Pos, code diag.Code, format string, args ...any) {
	l.report(pos, &diag.Diagnostic{Code: code, Msg: fmt.Sprintf(format, args...)})
}

// report reports d at pos, for an error with an explanation of its own
// (d.Text and d.Fix).
func (l *Lowerer) report(pos token.Pos, d *diag.Diagnostic) {
	if pos.IsValid() {
		d.Pos = l.fset.Position(pos)
	}
	l.errs = append(l.errs, d)
}

// internalf reports a failure of the compiler itself: a construct the type
// checker accepted that lowering does not expect.
func (l *Lowerer) internalf(pos token.Pos, format string, args ...any) {
	l.errorf(pos, diag.InternalError, "internal compiler error: "+format, args...)
}

// unsupportedf reports a valid construct that the compiler cannot compile
// yet.
func (l *Lowerer) unsupportedf(pos token.Pos, format string, args ...any) {
	l.errorf(pos, diag.NotYetSupported, format, args...)
}

// ----------------------------------------------------------------------------
// Symbols

// pkgPath returns the import path used in symbols for pkg.
func pkgPath(pkg *types2.Package) string {
	if pkg == nil {
		return "builtin"
	}
	return pkg.Path()
}

// typeString formats a type for symbol names: fully qualified by import
// path, with types declared in functions (and their instances in generic
// functions, see typeMap.local) told apart by position.
func typeString(T types2.Type) string {
	return types2.SymbolString(T, func(p *types2.Package) string { return p.Path() })
}

// funcSymName returns the symbol name of a function or method instance.
// targs are the receiver type's arguments followed by the function's own.
// They are listed after the name (pkg.F[int], pkg.(*Stack).Push[int]),
// except that a method with type parameters of its own on a generic type
// lists the receiver's arguments on the receiver: pkg.(*Vec[string]).Map[int].
func funcSymName(fn *types2.Func, targs []types2.Type) string {
	fn = fn.Origin()
	var sb strings.Builder
	sb.WriteString(pkgPath(fn.Pkg()))
	sb.WriteByte('.')
	sig := fn.Type().(*types2.Signature)
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		ptr := false
		if p, ok := rt.(*types2.Pointer); ok {
			rt = p.Elem()
			ptr = true
		}
		name := rt.String()
		if n, ok := types2.Unalias(rt).(*types2.Named); ok {
			name = n.Obj().Name()
			if tn := n.Obj(); tn.Parent() != nil && tn.Pkg() != nil && tn.Parent() != tn.Pkg().Scope() {
				// Methods of types declared in functions (enum String
				// methods) need names that tell such types apart.
				name += fmt.Sprintf("·%d", tn.Pos())
			}
		}
		if nr := sig.RecvTypeParams().Len(); nr > 0 && sig.TypeParams().Len() > 0 && len(targs) >= nr {
			name += "[" + typeList(targs[:nr]) + "]"
			targs = targs[nr:]
		}
		if ptr {
			sb.WriteString("(*" + name + ")")
		} else {
			sb.WriteString(name)
		}
		sb.WriteByte('.')
	}
	sb.WriteString(fn.Name())
	if len(targs) > 0 {
		sb.WriteString("[" + typeList(targs) + "]")
	}
	return sb.String()
}

// typeList formats type arguments for symbol names, separated by commas.
func typeList(targs []types2.Type) string {
	var sb strings.Builder
	for i, t := range targs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(typeString(t))
	}
	return sb.String()
}

// externName returns the symbol a body-less function binds to: its
// //pu:extern name, or its Plutonium symbol for assembly stubs.
func externName(d *ast.FuncDecl) (string, bool) {
	for _, dir := range directives(d.Doc) {
		if name, ok := strings.CutPrefix(dir, "extern "); ok {
			return strings.TrimSpace(name), true
		}
	}
	return "", false
}

// directives returns the //pu: directives in a doc comment, without the prefix.
func directives(doc *ast.CommentGroup) []string {
	if doc == nil {
		return nil
	}
	var out []string
	for _, c := range doc.List {
		if d, ok := strings.CutPrefix(c.Text, "//pu:"); ok {
			out = append(out, strings.TrimSpace(d))
		}
	}
	return out
}

// funcSym returns the symbol for a call to fn instantiated with targs (and the
// receiver's type arguments for methods of generic types), queueing its body
// for lowering the first time it is seen.
func (l *Lowerer) funcSym(fn *types2.Func, targs []types2.Type) *ssa.Sym {
	fn = fn.Origin()
	d := l.decls[fn]
	if d == nil {
		if en := enumOfMethod(fn); en != nil {
			return l.enumString(fn, en)
		}
	}
	if d != nil && d.decl.Body == nil {
		if name, ok := externName(d.decl); ok {
			sym := l.out.Sym(name, ssa.SymExtern)
			l.cfuncs[sym] = true
			l.symFuncs[sym] = fn
			return sym
		}
		sym := l.out.Sym(funcSymName(fn, targs), ssa.SymExtern)
		l.symFuncs[sym] = fn
		return sym
	}
	name := funcSymName(fn, targs)
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	l.symFuncs[sym] = fn
	if d != nil && hasDirective(d.decl.Doc, "interrupt") {
		l.interruptSyms[sym] = true
	}
	if d == nil {
		l.internalf(token.NoPos, "no source for function %s", name)
		return sym
	}
	sig := fn.Type().(*types2.Signature)
	var tm *typeMap
	if len(targs) > 0 {
		tm = newTypeMap(l, fn, targs)
		if sig.Recv() != nil {
			sig = l.methodSig(fn, targs)
		} else {
			sig = tm.subst(sig).(*types2.Signature)
		}
	}
	l.queue = append(l.queue, &funcJob{sym: sym, pkg: d.pkg, decl: d.decl, sig: sig, tmap: tm})
	return sym
}

// lowerExports queues every function marked //pu:export, which assembly or C
// may call without any Plutonium code referring to it.
func (l *Lowerer) lowerExports() {
	for _, p := range l.prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				for _, dir := range directives(fd.Doc) {
					name, ok := strings.CutPrefix(dir, "export")
					if !ok || name != "" && name[0] != ' ' {
						continue
					}
					fn, _ := p.Info.Defs[fd.Name].(*types2.Func)
					if fn == nil {
						continue
					}
					if sig := fn.Type().(*types2.Signature); sig.TypeParams() != nil || sig.RecvTypeParams() != nil {
						l.errorf(fd.Pos(), diag.GenericExport, "cannot export generic function %s", fn.Name())
						continue
					}
					sym := l.funcSym(fn, nil)
					if name = strings.TrimSpace(name); name != "" && name != sym.Name {
						l.cExport(fd, fn, sym, name)
					}
				}
			}
		}
	}
}

// ----------------------------------------------------------------------------
// Type parameter substitution

// A typeMap replaces a generic function's type parameters (including those of
// a generic receiver type) with concrete types.
type typeMap struct {
	l       *Lowerer
	tparams []*types2.TypeParam
	targs   []types2.Type

	// The source range of the generic function: types declared in it
	// are instantiated with it (see local).
	start, end token.Pos
}

// A localKey names an instance of a type declared in a generic function.
type localKey struct {
	obj   *types2.TypeName
	targs string
}

func newTypeMap(l *Lowerer, fn *types2.Func, targs []types2.Type) *typeMap {
	sig := fn.Type().(*types2.Signature)
	var tps []*types2.TypeParam
	if rtp := sig.RecvTypeParams(); rtp != nil {
		for i := 0; i < rtp.Len(); i++ {
			tps = append(tps, rtp.At(i))
		}
	}
	if tp := sig.TypeParams(); tp != nil {
		for i := 0; i < tp.Len(); i++ {
			tps = append(tps, tp.At(i))
		}
	}
	if len(tps) != len(targs) {
		panic(fmt.Sprintf("lower: %s has %d type parameters, got %d type arguments", fn, len(tps), len(targs)))
	}
	m := &typeMap{l: l, tparams: tps, targs: targs}
	if d := l.decls[fn.Origin()]; d != nil {
		m.start, m.end = d.decl.Pos(), d.decl.End()
	}
	return m
}

func (m *typeMap) subst(T types2.Type) types2.Type {
	if m == nil || T == nil {
		return T
	}
	return types2.SubstLocal(m.l.ctxt, T, m.tparams, m.targs, m.local)
}

// local instantiates a type declared inside the generic function, which
// may mention the function's type parameters: each instance of the
// function has its own copy of the type, as in Go. It returns nil for any
// other named type.
func (m *typeMap) local(t *types2.Named) types2.Type {
	obj := t.Obj()
	if obj.Pos() < m.start || obj.Pos() >= m.end || obj.Pkg() == nil || obj.Parent() == obj.Pkg().Scope() {
		return nil
	}
	key := localKey{obj, typeList(m.targs)}
	if n := m.l.localTypes[key]; n != nil {
		return n
	}
	// The instance keeps the declaration's position, so localSuffix
	// still tells it apart from types of the same name elsewhere; the
	// type arguments in its name tell the instances apart.
	tn := types2.NewTypeName(obj.Pos(), obj.Pkg(), obj.Name()+"["+key.targs+"]", nil)
	n := types2.NewNamed(tn, nil, nil)
	m.l.localTypes[key] = n
	n.SetUnderlying(m.subst(t.Underlying()))
	return n
}

// ----------------------------------------------------------------------------
// String and data symbols

// stringData returns a read-only symbol holding s's bytes.
func (l *Lowerer) stringData(s string) *ssa.Sym {
	if sym := l.strs[s]; sym != nil {
		return sym
	}
	sym := l.out.Sym(fmt.Sprintf(".str.%d", len(l.strs)), ssa.SymData)
	l.strs[s] = sym
	l.out.Data = append(l.out.Data, &ssa.Data{
		Sym: sym, Size: int64(len(s)), Align: 1, Bytes: []byte(s), ReadOnly: true, Local: true,
	})
	return sym
}

// closureRecord returns a read-only one-word closure record for calling the
// function fn through a func value.
func (l *Lowerer) closureRecord(fn *ssa.Sym) *ssa.Sym {
	if rec := l.closureRecords[fn]; rec != nil {
		return rec
	}
	rec := l.out.Sym(fn.Name+"·f", ssa.SymData)
	l.closureRecords[fn] = rec
	l.out.Data = append(l.out.Data, &ssa.Data{
		Sym: rec, Size: 8, Align: 8, ReadOnly: true,
		Relocs: []ssa.DataReloc{{Off: 0, Sym: fn}},
	})
	return rec
}
