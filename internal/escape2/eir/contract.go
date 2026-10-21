package eir

import (
	"github.com/matjam/plutonium/internal/constant"
	"github.com/matjam/plutonium/internal/escape2/contract"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// The functions internal/lower expands inline instead of calling (lower/
// call.go callResults, in its order): the interrupt gate constructors,
// errors.New, the intrinsics, the count intrinsics of pluto/rt,
// strings.Slice and strings.SliceFrom, and the runtime's call of the panic
// handler.

// qualifiedName returns "path.Name" for a package-level function.
func qualifiedName(fn *types2.Func) string {
	if fn.Pkg() == nil {
		return fn.Name()
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// inlineContract returns the contract table entry of a call of fn that the
// lowerer expands inline, if it does.
func (b *builder) inlineContract(fn *types2.Func, e *ast.CallExpr) (string, bool) {
	if fn.Pkg() == nil || fn.Type().(*types2.Signature).Recv() != nil {
		return "", false
	}
	if types2.IsErrorsNew(fn) {
		return "pluto/errors.New", true
	}
	name := qualifiedName(fn)
	switch name {
	case "pluto/strings.Slice":
		if len(e.Args) != 3 {
			return "", false
		}
	case "pluto/strings.SliceFrom":
		if len(e.Args) != 2 {
			return "", false
		}
	}
	if contract.IsInline(name) || contract.IsTrusted(name) {
		return name, true
	}
	return "", false
}

// contractInline reports whether a contract entry is an inline expansion.
func (b *builder) contractInline(name string) bool { return contract.IsInline(name) }

// isPanicHook reports whether fn is pluto/rt.callPanicHandler, which the
// lowerer turns into a call of the program's panic handler.
func (b *builder) isPanicHook(fn *types2.Func) bool {
	return qualifiedName(fn) == contract.PanicHook
}

// panicHandler returns the program's panic handler: the function marked
// //pu:panic, or pluto/rt.defaultPanic (lower/panic.go findPanicHandler).
func (b *builder) panicHandler() *types2.Func {
	if b.panicFn != nil || b.panicDone {
		return b.panicFn
	}
	b.panicDone = true
	for _, p := range b.prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Doc == nil {
					continue
				}
				for _, c := range fd.Doc.List {
					if c.Text == "//pu:panic" {
						if fn, ok := b.defs[fd.Name].(*types2.Func); ok {
							b.panicFn = fn
							return fn
						}
					}
				}
			}
		}
	}
	b.panicFn = b.rtFunc("defaultPanic")
	return b.panicFn
}

// isGateFunc reports whether fn is x86.InterruptGate or x86.TrapGate, which
// the lowerer expands into a call of x86.gateAt (lower/interrupt.go).
func isGateFunc(fn *types2.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == "pluto/arch/x86" && (fn.Name() == "InterruptGate" || fn.Name() == "TrapGate")
}

// constInt returns the value of an integer constant.
func constInt(c constant.Value) (int64, bool) {
	return constant.Int64Val(constant.ToInt(c))
}
