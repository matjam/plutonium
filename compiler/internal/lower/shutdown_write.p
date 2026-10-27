// Translated by go2pu from internal/lower/shutdown_write.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// These existing library ABI operations write typed memory without a source
// assignment: assembly atomics and kernel output buffers. A function-symbol
// adapter covers direct calls and function values alike. Raw Syscall remains
// an unsafe interface, not an inferred foreign-memory contract.
func staticWriteParam(fn *types2.Func) (index int, poll bool, ok bool) {
	if fn.Pkg() == nil || fn.Type().(*types2.Signature).Recv() != nil {
		return
	}
	switch fn.Pkg().Path() {
	case "pluto/sync/atomic":
		for _, prefix := range []string{"Store", "Add", "Swap", "CompareAndSwap", "And", "Or"} {
			if suffix, found := strings.CutPrefix(fn.Name(), prefix); found {
				switch suffix {
				case "Int32", "Uint32", "Int64", "Uint64", "Uintptr":
					return 0, false, true
				case "Pointer":
					return 0, false, prefix == "Store" || prefix == "Swap" || prefix == "CompareAndSwap"
				}
			}
		}
	case "pluto/syscall":
		switch fn.Name() {
		case "Read", "Fstat", "Getdents64", "Wait4":
			return 1, false, true
		case "Getrandom", "Getcwd", "Pipe2":
			return 0, false, true
		case "Fstatat", "Readlinkat":
			return 2, false, true
		case "Socketpair":
			return 3, false, true
		case "Poll":
			return 0, true, true
		}
	}
	return
}

func (l *Lowerer) staticWriteSym(fn *types2.Func, targs []types2.Type) *ssa.Sym {
	index, poll, ok := staticWriteParam(fn)
	if !ok || l.rtFuncOrNil("staticCleanupCheck") == nil {
		return nil
	}
	name := compat.Concat(funcSymName(fn, targs), "·shutdownwrite")
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name], l.symFuncs[sym] = sym, fn
	impl := l.rawFuncSym(fn, targs)
	sig := fn.Type().(*types2.Signature)
	job := pu.New(funcJob{sym: sym, sig: sig, build: pu.New(goClosure_staticWriteSym_1{sig: sig, index: index, poll: poll, l: l, fn: fn, impl: impl})})
	// Preserve the ABI boundary even when no static place needs protection:
	// emitCall strips generation tags for a raw external implementation.
	// Empty-inventory elision removes only the provisional guard inside it.
	l.queue = compat.Append(l.queue, job)
	return sym
}

// goClosure_staticWriteSym_1 is the function literal at shutdown_write.go:61, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_staticWriteSym_1 struct {
	sig   *types2.Signature
	index int
	poll  bool
	l     *Lowerer
	fn    *types2.Func
	impl  *ssa.Sym
}

func (goSelf *goClosure_staticWriteSym_1) Call(s *fnState) {
	in := s.incomingArgs(goSelf.sig)
	v := in[goSelf.index]
	switch u := v.T.Underlying().(type) {
	case *types2.Pointer:
		s.ifThen(s.newValue(ssa.OpNe, ssa.TBool, v.v, s.constInt(ssa.TPtr, 0)), func() {
			s.staticWriteType(v.v, u.Elem())
		})
	case *types2.Slice:
		p, n, _ := s.sliceParts(v)
		if goSelf.poll {
			st := u.Elem().Underlying().(*types2.Struct)
			fields := compat.Make[*types2.Var](st.NumFields())
			for i := range fields {
				fields[i] = st.Field(i)
			}
			offs := goSelf.l.sizes.Offsetsof(fields)
			guarded := false
			for i, f := range fields {
				if f.Name() == "Revents" {
					guarded = true
					s.countedLoop(n, nil, func(j *ssa.Value) {
						s.staticWriteType(s.offPtr(s.elemAddr(p, j, u.Elem()), offs[i]), f.Type())
					})
				}
			}
			if !guarded {
				goSelf.l.internalf(token.NoPos, "typed Poll output contract has no Revents field")
			}
		} else {
			bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, goSelf.l.sizeof(u.Elem())))
			s.staticWriteCheck(p, bytes)
		}
	default:
		goSelf.l.internalf(token.NoPos, "typed output contract for %s has no pointer or slice destination", pu.Box(goSelf.fn.FullName()))
	}
	res := s.emitCall(callee{sym: goSelf.impl}, goSelf.sig, nil, in, token.NoPos)
	s.returnValues(res)
}
