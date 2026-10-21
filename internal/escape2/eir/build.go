package eir

import (
	"fmt"
	"sort"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Build desugars every function of a loaded program into EIR: declared
// functions and methods (generic ones once, by origin), literals, range
// bodies, package initializers and the synthesized functions the lowerer
// emits. A construct the desugarer does not know is an internal compiler
// error, never a silent omission.
func Build(prog *load.Program) (*Program, error) {
	return BuildWith(prog, Options{})
}

// Options are the inputs Build computes itself when they are not given.
type Options struct {
	// Moves is the move checker's account of the program, which says where
	// owners are known to hold nothing and so are not dropped (section
	// 18.6), as internal/lower uses it. Nil runs the move checker with no
	// facts about calls (moves.AnalyzeWith(prog, nil)).
	Moves *moves.Info
	// Scoped are the scoped tasks of sync.Scope (section 19.4). Nil runs
	// scoped.Analyze.
	Scoped *scoped.Info
}

// BuildWith is Build with inputs supplied by the caller.
func BuildWith(prog *load.Program, opt Options) (*Program, error) {
	b := newBuilder(prog, opt)
	b.declare()
	b.analyzeGoexit()
	b.run()
	if len(b.errs) > 0 {
		sort.SliceStable(b.errs, func(i, j int) bool { return b.errs[i].Pos.Offset < b.errs[j].Pos.Offset })
		return b.p, b.errs.Err()
	}
	return b.p, nil
}

// builder builds a Program.
type builder struct {
	prog *load.Program
	fset *token.FileSet
	p    *Program

	// Type information of every package, merged: AST nodes are unique.
	types      map[ast.Expr]types2.TypeAndValue
	uses       map[*ast.Ident]types2.Object
	defs       map[*ast.Ident]types2.Object
	selections map[*ast.SelectorExpr]*types2.Selection
	implicits  map[ast.Node]types2.Object
	instances  map[*ast.Ident]types2.Instance

	pkgOf  map[*types2.Package]*load.Package
	decls  map[*types2.Func]*declRef
	moves  *moves.Info
	scoped *scoped.Info

	synths map[synthKey][]*Synth // every synthesized function, by kind and method
	queue  []func()              // bodies still to build

	ctxt      *types2.Context
	litFns    map[*ast.FuncLit]*fnb
	panicFn   *types2.Func
	panicDone bool
	noRet     map[*types2.Func]int // noreturn.go
	gx        *goexitInfo          // goexit.go

	dropNeeds typeMap[bool] // needsDrop
	countedT  typeMap[bool] // counted
	itabs     itabState     // itabs.go

	errs diag.List
}

// declRef locates a declared function's source.
type declRef struct {
	pkg  *load.Package
	decl *ast.FuncDecl
}

func newBuilder(prog *load.Program, opt Options) *builder {
	b := &builder{
		prog:       prog,
		fset:       prog.Fset,
		p:          &Program{Sources: map[FuncKey]FuncID{}},
		types:      map[ast.Expr]types2.TypeAndValue{},
		uses:       map[*ast.Ident]types2.Object{},
		defs:       map[*ast.Ident]types2.Object{},
		selections: map[*ast.SelectorExpr]*types2.Selection{},
		implicits:  map[ast.Node]types2.Object{},
		instances:  map[*ast.Ident]types2.Instance{},
		pkgOf:      map[*types2.Package]*load.Package{},
		decls:      map[*types2.Func]*declRef{},
		moves:      opt.Moves,
		ctxt:       types2.NewContext(),
		litFns:     map[*ast.FuncLit]*fnb{},
		noRet:      map[*types2.Func]int{},
		scoped:     opt.Scoped,
	}
	for _, p := range prog.Packages {
		b.pkgOf[p.Types] = p
		for k, v := range p.Info.Types {
			b.types[k] = v
		}
		for k, v := range p.Info.Uses {
			b.uses[k] = v
		}
		for k, v := range p.Info.Defs {
			b.defs[k] = v
		}
		for k, v := range p.Info.Selections {
			b.selections[k] = v
		}
		for k, v := range p.Info.Implicits {
			b.implicits[k] = v
		}
		for k, v := range p.Info.Instances {
			b.instances[k] = v
		}
	}
	if b.scoped == nil {
		b.scoped = scoped.Analyze(prog)
	}
	if b.moves == nil {
		b.moves, _ = moves.AnalyzeWith(prog, nil)
	}
	return b
}

// errorf records an internal compiler error: a construct the desugarer
// does not know.
func (b *builder) errorf(pos token.Pos, format string, args ...any) {
	b.errs = append(b.errs, diag.New(b.fset.Position(pos), diag.InternalError, "internal compiler error: escape2: %s: %s", b.fset.Position(pos), fmt.Sprintf(format, args...)))
}

// newFunc adds a function to the program, with no body yet.
func (b *builder) newFunc(k FuncKey, name string, pkg *types2.Package, sig *types2.Signature, pos, end token.Pos) *Func {
	f := &Func{ID: FuncID(len(b.p.Funcs)), Key: k, Name: name, Pkg: pkg, Sig: sig, Pos: pos, End: end}
	if sig != nil {
		f.Results = sig.Results().Len()
	}
	b.p.Funcs = append(b.p.Funcs, f)
	b.p.Sources[k] = f.ID
	return f
}

// declare adds every declared function and package initialization, so
// that calls can name them before their bodies are built, and queues their
// bodies.
func (b *builder) declare() {
	for _, p := range b.prog.Packages {
		for _, file := range p.Files {
			for _, d := range file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				obj, ok := b.defs[fd.Name].(*types2.Func)
				if !ok {
					continue
				}
				b.decls[obj] = &declRef{pkg: p, decl: fd}
				f := b.newFunc(FuncKey{Obj: obj}, funcName(obj), p.Types, obj.Type().(*types2.Signature), fd.Pos(), fd.End())
				f.External = isExternalDecl(fd) || (p == b.prog.Main && obj.Name() == "main" && fd.Recv == nil)
				if fd.Body == nil {
					f.Bodyless = true
				}
			}
		}
		f := b.newFunc(FuncKey{Init: p.Types}, p.Path+" package initialization", p.Types, types2.NewSignatureType(nil, nil, nil, nil, nil, false), token.NoPos, token.NoPos)
		f.External = true
	}
	for _, f := range append([]*Func(nil), b.p.Funcs...) {
		switch {
		case f.Key.Obj != nil:
			d := b.decls[f.Key.Obj]
			if d.decl.Body == nil {
				b.queue = append(b.queue, func() {
					if b.hookBody(f, d) {
						f.Bodyless = false
						return
					}
					b.buildBodyless(f)
				})
				continue
			}
			b.queue = append(b.queue, func() { b.buildDecl(f, d) })
		case f.Key.Init != nil:
			b.queue = append(b.queue, func() { b.buildInit(f, b.pkgOf[f.Key.Init]) })
		}
	}
}

// run builds queued bodies until none is left; building a body may queue
// synthesized functions, and then the itab adapters of interfaces asserted
// to (itabs.go), whose bodies may queue more.
func (b *builder) run() {
	for {
		for len(b.queue) > 0 {
			job := b.queue[0]
			b.queue = b.queue[1:]
			job()
		}
		if !b.finishItabs() {
			return
		}
	}
}

// isExternalDecl reports whether a declaration is called from outside
// Plutonium code: //pu:export, //pu:interrupt, //pu:panic, //pu:scheduler.
func isExternalDecl(fd *ast.FuncDecl) bool {
	if fd.Doc == nil {
		return false
	}
	for _, c := range fd.Doc.List {
		for _, d := range []string{"//pu:export", "//pu:interrupt", "//pu:panic"} {
			if len(c.Text) >= len(d) && c.Text[:len(d)] == d {
				return true
			}
		}
	}
	return false
}

// funcName names a declared function for messages: "pkg.F", "pkg.(*T).M".
func funcName(obj *types2.Func) string {
	pkg := ""
	if obj.Pkg() != nil {
		pkg = obj.Pkg().Path() + "."
	}
	sig := obj.Type().(*types2.Signature)
	if r := sig.Recv(); r != nil {
		t := r.Type()
		ptr := ""
		if p, ok := t.(*types2.Pointer); ok {
			t = p.Elem()
			ptr = "*"
		}
		if n, ok := types2.Unalias(t).(*types2.Named); ok {
			return fmt.Sprintf("%s(%s%s).%s", pkg, ptr, n.Obj().Name(), obj.Name())
		}
		return pkg + obj.Name()
	}
	return pkg + obj.Name()
}

// funcOf returns the ID of a declared function, by origin; NoFunc for a
// function the program does not declare (an interface method, a built-in
// box method).
func (b *builder) funcOf(fn *types2.Func) FuncID {
	if fn == nil {
		return NoFunc
	}
	fn = fn.Origin()
	if id := b.p.Lookup(FuncKey{Obj: fn}); id != NoFunc {
		return id
	}
	sig, _ := fn.Type().(*types2.Signature)
	if sig == nil || fn.Pkg() == nil || sig.Recv() == nil || isInterface(sig.Recv().Type()) {
		return NoFunc
	}
	// A method the type checker declares and the lowerer gives a body:
	// the String method of an enum type, the Error method of an
	// errors.New call site's type. Both return a constant string.
	f := b.newFunc(FuncKey{Obj: fn}, funcName(fn), fn.Pkg(), sig, fn.Pos(), fn.Pos())
	b.queue = append(b.queue, func() { b.buildConstMethod(f) })
	return f.ID
}

// buildConstMethod builds a method that returns constants: static memory.
func (b *builder) buildConstMethod(f *Func) {
	fb := b.newFnb(f, nil, nil)
	fb.addressed = map[*types2.Var]bool{}
	if r := f.Sig.Recv(); r != nil {
		f.Inputs = append(f.Inputs, Input{Var: r, Type: r.Type()})
		fb.input(0)
	}
	for i := 0; i < f.Sig.Results().Len(); i++ {
		if hasPointers(f.Sig.Results().At(i).Type()) {
			fb.sink(SinkReturn, fb.heap(HeapStatic), i, f.Pos)
		}
	}
	fb.cur = nil
	fb.finish()
}

// synth returns the synthesized function s names, adding it, and queueing
// its body, the first time.
func (b *builder) synth(s Synth) FuncID {
	k := synthKey{s.Kind, s.Method}
	for _, t := range b.synths[k] {
		if sameSynth(t, &s) {
			return b.p.Sources[FuncKey{Synth: t}]
		}
	}
	sp := &Synth{Kind: s.Kind, Recv: s.Recv, Method: s.Method, Path: append([]int(nil), s.Path...), Site: s.Site}
	if b.synths == nil {
		b.synths = map[synthKey][]*Synth{}
	}
	b.synths[k] = append(b.synths[k], sp)
	sig := b.synthSig(sp)
	var pkg *types2.Package
	if sp.Method != nil {
		pkg = sp.Method.Pkg()
	}
	f := b.newFunc(FuncKey{Synth: sp}, synthName(sp), pkg, sig, token.NoPos, token.NoPos)
	b.queue = append(b.queue, func() { b.buildSynth(f) })
	return f.ID
}

// sameSynth reports whether two Synths name the same function: the same
// kind, method, path and site, and identical types.
// synthKey buckets synthesized functions.
type synthKey struct {
	kind   SynthKind
	method *types2.Func
}

func sameSynth(a, b *Synth) bool {
	if a.Kind != b.Kind || a.Method != b.Method || a.Site != b.Site || len(a.Path) != len(b.Path) {
		return false
	}
	for i := range a.Path {
		if a.Path[i] != b.Path[i] {
			return false
		}
	}
	if a.Recv == nil || b.Recv == nil {
		return a.Recv == b.Recv
	}
	return types2.Identical(a.Recv, b.Recv)
}

// synthName names a synthesized function for messages and dumps.
func synthName(s *Synth) string {
	m := ""
	if s.Method != nil {
		m = s.Method.Name()
	}
	switch s.Kind {
	case SynthPromoted:
		return fmt.Sprintf("%s.%s (promoted)", s.Recv, m)
	case SynthPtrWrapper:
		return fmt.Sprintf("%s.%s (pointer wrapper)", s.Recv, m)
	case SynthItabAdapter:
		return fmt.Sprintf("%s.%s (interface adapter)", s.Recv, m)
	case SynthMethodValue:
		return fmt.Sprintf("%s.%s (method value)", s.Recv, m)
	case SynthMethodExpr:
		return fmt.Sprintf("(%s).%s (method expression)", s.Recv, m)
	case SynthIfaceMethodValue:
		return fmt.Sprintf("%s.%s (interface method value)", s.Recv, m)
	case SynthVariant:
		return fmt.Sprintf("%s.%s (variant)", s.Recv, variantName(s))
	case SynthDropGlue:
		return fmt.Sprintf("%s (drop)", s.Recv)
	case SynthTaskThunk:
		return "task thunk"
	case SynthDeferRecord:
		return "deferred calls"
	}
	return "synthesized function"
}
