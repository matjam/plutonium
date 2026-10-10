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

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Config controls lowering.
type Config struct {
	Freestanding bool
	Sizes        types2.Sizes
}

// An Error is a lowering error at a source position.
type Error struct {
	Pos token.Position
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

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
	globals        map[*types2.Var]*ssa.Sym
	strs           map[string]*ssa.Sym
	closureRecords map[*ssa.Sym]*ssa.Sym // static closure record per function used as a value
	exports        map[*ssa.Sym][]string // extra symbol names from //pu:export
	statics        int                   // anonymous static objects created
	types          *typeTable

	errs []error
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
func Lower(cfg *Config, prog *load.Program) (*ssa.Program, error) {
	l := &Lowerer{
		cfg:            cfg,
		prog:           prog,
		fset:           prog.Fset,
		sizes:          cfg.Sizes,
		out:            ssa.NewProgram(),
		ctxt:           types2.NewContext(),
		pkgOf:          map[*types2.Package]*load.Package{},
		decls:          map[*types2.Func]*funcDecl{},
		funcs:          map[string]*ssa.Sym{},
		globals:        map[*types2.Var]*ssa.Sym{},
		strs:           map[string]*ssa.Sym{},
		closureRecords: map[*ssa.Sym]*ssa.Sym{},
		exports:        map[*ssa.Sym][]string{},
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

	l.lowerGlobals()
	l.lowerEntry()
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
	if len(l.errs) > 0 {
		return nil, joinErrors(l.errs)
	}
	sort.Slice(l.out.Funcs, func(i, j int) bool { return l.out.Funcs[i].Name < l.out.Funcs[j].Name })
	return l.out, nil
}

func (l *Lowerer) errorf(pos token.Pos, format string, args ...any) {
	l.errs = append(l.errs, &Error{Pos: l.fset.Position(pos), Msg: fmt.Sprintf(format, args...)})
}

type errorList []error

func (e errorList) Error() string {
	var sb strings.Builder
	for i, err := range e {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(err.Error())
	}
	return sb.String()
}

func joinErrors(errs []error) error { return errorList(errs) }

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
// path, with types declared in functions told apart by position.
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
			return l.out.Sym(name, ssa.SymExtern)
		}
		return l.out.Sym(funcSymName(fn, targs), ssa.SymExtern)
	}
	name := funcSymName(fn, targs)
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	if d == nil {
		l.errs = append(l.errs, fmt.Errorf("lower: no source for function %s", name))
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
					if fn.Type().(*types2.Signature).TypeParams() != nil {
						l.errorf(fd.Pos(), "cannot export generic function %s", fn.Name())
						continue
					}
					sym := l.funcSym(fn, nil)
					if name = strings.TrimSpace(name); name != "" && name != sym.Name {
						l.exports[sym] = append(l.exports[sym], name)
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
	return &typeMap{l: l, tparams: tps, targs: targs}
}

func (m *typeMap) subst(T types2.Type) types2.Type {
	if m == nil || T == nil {
		return T
	}
	return types2.Subst(m.l.ctxt, T, m.tparams, m.targs)
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
