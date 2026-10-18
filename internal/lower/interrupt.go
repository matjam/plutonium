package lower

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Interrupt handlers (//pu:interrupt, abi.md section 10.4) are entered by the
// CPU, not called: the backend gives them an entry and exit sequence that
// saves every register and returns with iretq. Lowering checks their
// signatures, rejects direct calls and func values, and expands
// x86.InterruptGate and x86.TrapGate, the only ways to put a handler's address
// in the interrupt table (spec section 20.8).

// The attribute byte each constructor of the interrupt table's entries puts in
// the entry: present, privilege level 0, and the gate type (abi.md section
// 10.5).
const (
	interruptGateAttr = 0x8E
	trapGateAttr      = 0x8F
)

// isGateFunc reports whether fn is x86.InterruptGate or x86.TrapGate.
func isGateFunc(fn *types2.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == x86Path && (fn.Name() == "InterruptGate" || fn.Name() == "TrapGate")
}

// hasDirective reports whether a doc comment carries //pu:name. The
// directive //pu:interrupt trap is a form of //pu:interrupt.
func hasDirective(doc *ast.CommentGroup, name string) bool {
	for _, d := range directives(doc) {
		if d == name || name == "interrupt" && d == "interrupt trap" {
			return true
		}
	}
	return false
}

// isTrapHandler reports whether a doc comment declares //pu:interrupt trap.
func isTrapHandler(doc *ast.CommentGroup) bool { return hasDirective(doc, "interrupt trap") }

// isTrapHandler reports whether fn is declared //pu:interrupt trap.
func (l *Lowerer) isTrapHandler(fn *types2.Func) bool {
	d := l.decls[fn.Origin()]
	return d != nil && isTrapHandler(d.decl.Doc)
}

// isInterrupt reports whether fn is declared //pu:interrupt, in either form.
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
		l.errorf(fd.Pos(), diag.InterruptDecl, "//pu:interrupt function %s must have a body", name)
	case sig.Recv() != nil:
		l.errorf(fd.Pos(), diag.InterruptDecl, "method %s cannot be an interrupt handler", name)
	case sig.TypeParams().Len() > 0:
		l.errorf(fd.Pos(), diag.InterruptDecl, "generic function %s cannot be an interrupt handler", name)
	case sig.Results().Len() > 0:
		l.errorf(fd.Pos(), diag.InterruptSig, "interrupt handler %s cannot have results", name)
	case hasDirective(fd.Doc, "inline"):
		l.errorf(fd.Pos(), diag.InterruptDecl, "interrupt handler %s cannot be //pu:inline", name)
	case sig.Params().Len() < 1 || sig.Params().Len() > 2 || sig.Variadic():
		l.errorf(fd.Pos(), diag.InterruptSig, "interrupt handler %s must take (frame *x86.InterruptFrame) or (frame *x86.InterruptFrame, code uint64)", name)
	case !isFramePointer(sig.Params().At(0).Type()):
		l.errorf(fd.Pos(), diag.InterruptSig, "interrupt handler %s: first parameter must be a pointer to the interrupt frame, not %s", name, sig.Params().At(0).Type())
	case sig.Params().Len() == 2 && !isErrorCode(sig.Params().At(1).Type()):
		l.errorf(fd.Pos(), diag.InterruptSig, "interrupt handler %s: error code must be uint64, not %s", name, sig.Params().At(1).Type())
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

// gateCall expands x86.InterruptGate(h, selector, ist) and
// x86.TrapGate(h, selector, ist): the table entry that enters handler h. The
// handler must be named in the call, and be of the form the constructor takes
// (spec section 20.8, abi.md section 10.5). The entry is built by the
// package's gateAt from h's address.
func (s *fnState) gateCall(e *ast.CallExpr, gate *types2.Func) []value {
	trap := gate.Name() == "TrapGate"
	sig := gate.Type().(*types2.Signature)
	T := sig.Results().At(0).Type()
	form := "//pu:interrupt function"
	if trap {
		form = "//pu:interrupt trap function"
	}
	var h *types2.Func
	if len(e.Args) == 3 {
		if id := identOf(ast.Unparen(e.Args[0])); id != nil {
			h, _ = s.info.Uses[id].(*types2.Func)
		}
	}
	switch {
	case h == nil || !s.l.isInterrupt(h):
		s.l.errorf(e.Pos(), diag.GateArg, "x86.%s: the first argument must name a %s", gate.Name(), form)
		return []value{s.zeroValue(T)}
	case s.l.isTrapHandler(h) != trap:
		is, other := "a //pu:interrupt function, not a trap handler", "x86.InterruptGate"
		if !trap {
			is, other = "a //pu:interrupt trap function", "x86.TrapGate"
		}
		s.l.errorf(e.Pos(), diag.GateArg, "x86.%s: %s is %s; install it with %s", gate.Name(), h.Name(), is, other)
		return []value{s.zeroValue(T)}
	}
	at, _ := gate.Pkg().Scope().Lookup("gateAt").(*types2.Func)
	if at == nil {
		s.l.internalf(e.Pos(), "pluto/arch/x86 has no gateAt")
		return []value{s.zeroValue(T)}
	}
	atSig := at.Type().(*types2.Signature)
	addr := s.funcAddr(s.l.funcSym(h, nil))
	attr := int64(interruptGateAttr)
	if trap {
		attr = trapGateAttr
	}
	p := atSig.Params()
	args := []value{
		scalarValue(s.newValue(ssa.OpBitcast, s.l.scalarType(p.At(0).Type()), addr), p.At(0).Type()),
		s.exprAs(e.Args[1], p.At(1).Type()),
		s.exprAs(e.Args[2], p.At(2).Type()),
		scalarValue(s.constInt(s.l.scalarType(p.At(3).Type()), attr), p.At(3).Type()),
	}
	return s.emitCall(callee{sym: s.l.funcSym(at, nil)}, atSig, nil, args, e.Pos())
}

// checkNotHandler reports a call or func value of an interrupt handler,
// which only the CPU may enter.
func (s *fnState) checkNotHandler(sym *ssa.Sym, pos token.Pos, what string) {
	if s.l.interruptSyms[sym] {
		s.l.errorf(pos, diag.InterruptHandlerV, "interrupt handler %s cannot be %s; use x86.InterruptGate or x86.TrapGate to enter it in the interrupt table", sym.Name, what)
	}
}
