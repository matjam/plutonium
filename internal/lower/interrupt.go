package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Interrupt handlers (//pu:interrupt, abi.md section 10.4) are entered by the
// CPU, not called: the backend gives them an entry and exit sequence that
// saves every register and returns with iretq. Lowering checks their
// signatures, rejects direct calls and func values, and expands
// x86.HandlerAddr, the only way to take a handler's address.

// handlerAddrFunc is the intrinsic that yields a handler's entry address.
const handlerAddrFunc = "pluto/arch/x86.HandlerAddr"

// hasDirective reports whether a doc comment carries //pu:name.
func hasDirective(doc *ast.CommentGroup, name string) bool {
	for _, d := range directives(doc) {
		if d == name {
			return true
		}
	}
	return false
}

// isInterrupt reports whether fn is declared //pu:interrupt.
func (l *Lowerer) isInterrupt(fn *types2.Func) bool {
	d := l.decls[fn.Origin()]
	return d != nil && hasDirective(d.decl.Doc, "interrupt")
}

// checkInterrupts validates the signature of every //pu:interrupt function,
// whether or not the program uses it.
func (l *Lowerer) checkInterrupts() {
	for _, p := range l.prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || !hasDirective(fd.Doc, "interrupt") {
					continue
				}
				fn, _ := p.Info.Defs[fd.Name].(*types2.Func)
				if fn != nil {
					l.checkInterrupt(fd, fn)
				}
			}
		}
	}
}

// checkInterrupt reports why fn cannot be an interrupt handler, if it cannot.
// A handler is a plain function with a body, no results, a pointer to the
// interrupt frame and, for the exceptions that push one, the error code.
func (l *Lowerer) checkInterrupt(fd *ast.FuncDecl, fn *types2.Func) {
	sig := fn.Type().(*types2.Signature)
	name := fn.Name()
	switch {
	case fd.Body == nil:
		l.errorf(fd.Pos(), "//pu:interrupt function %s must have a body", name)
	case sig.Recv() != nil:
		l.errorf(fd.Pos(), "method %s cannot be an interrupt handler", name)
	case sig.TypeParams().Len() > 0:
		l.errorf(fd.Pos(), "generic function %s cannot be an interrupt handler", name)
	case sig.Results().Len() > 0:
		l.errorf(fd.Pos(), "interrupt handler %s cannot have results", name)
	case hasDirective(fd.Doc, "inline"):
		l.errorf(fd.Pos(), "interrupt handler %s cannot be //pu:inline", name)
	case sig.Params().Len() < 1 || sig.Params().Len() > 2 || sig.Variadic():
		l.errorf(fd.Pos(), "interrupt handler %s must take (frame *x86.InterruptFrame) or (frame *x86.InterruptFrame, code uint64)", name)
	case !isFramePointer(sig.Params().At(0).Type()):
		l.errorf(fd.Pos(), "interrupt handler %s: first parameter must be a pointer to the interrupt frame, not %s", name, sig.Params().At(0).Type())
	case sig.Params().Len() == 2 && !isErrorCode(sig.Params().At(1).Type()):
		l.errorf(fd.Pos(), "interrupt handler %s: error code must be uint64, not %s", name, sig.Params().At(1).Type())
	}
}

// isFramePointer reports whether T can hold the interrupt frame's address:
// any pointer, unsafe.Pointer or uintptr.
func isFramePointer(T types2.Type) bool {
	switch u := T.Underlying().(type) {
	case *types2.Pointer:
		return true
	case *types2.Basic:
		return u.Kind() == types2.UnsafePointer || u.Kind() == types2.Uintptr
	}
	return false
}

// isErrorCode reports whether T can hold the 8-byte error code.
func isErrorCode(T types2.Type) bool {
	u, ok := T.Underlying().(*types2.Basic)
	return ok && (u.Kind() == types2.Uint64 || u.Kind() == types2.Uintptr)
}

// handlerAddr expands x86.HandlerAddr(h): the address of handler h's code.
func (s *fnState) handlerAddr(e *ast.CallExpr, T types2.Type) value {
	var fn *types2.Func
	if len(e.Args) == 1 {
		if id := identOf(ast.Unparen(e.Args[0])); id != nil {
			fn, _ = s.info.Uses[id].(*types2.Func)
		}
	}
	if fn == nil || !s.l.isInterrupt(fn) {
		s.l.errorf(e.Pos(), "x86.HandlerAddr: argument must name a //pu:interrupt function")
		return s.zeroValue(T)
	}
	addr := s.funcAddr(s.l.funcSym(fn, nil))
	return scalarValue(s.newValue(ssa.OpBitcast, s.l.scalarType(T), addr), T)
}

// checkNotHandler reports a call or func value of an interrupt handler,
// which only the CPU may enter.
func (s *fnState) checkNotHandler(sym *ssa.Sym, pos token.Pos, what string) {
	if s.l.interruptSyms[sym] {
		s.l.errorf(pos, "interrupt handler %s cannot be %s; use x86.HandlerAddr for its address", sym.Name, what)
	}
}
