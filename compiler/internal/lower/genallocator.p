// Translated by go2pu from internal/lower/genallocator.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// genAllocatorSym gives custom allocator methods a registration wrapper.
// All direct, interface and method-value references pass through funcSym,
// including bodyless methods. The wrapper calls the original symbol once;
// Alloc registration runs after the implementation's deferred unlocks.
func (l *Lowerer) genAllocatorSym(gofn *types2.Func, targs []types2.Type) *ssa.Sym {
	fn := pu.New(gofn)

	if !l.cfg.GenCheck || (*fn).Name() != "Alloc" && (*fn).Name() != "Free" || (*fn).Pkg() == nil {
		return nil
	}
	(*fn) = (*fn).Origin()
	sig := pu.New((*fn).Type().(*types2.Signature))
	if (*sig).Recv() == nil {
		return nil
	}
	if len(targs) > 0 {
		(*sig) = l.methodSig((*fn), targs)
	}
	T := (*sig).Recv().Type()
	base := T
	if p, ok := base.(*types2.Pointer); ok {
		base = p.Elem()
	}
	if n, ok := base.(*types2.Named); ok && n.Obj().Pkg() != nil {
		pkg, name := n.Obj().Pkg().Path(), n.Obj().Name()
		if pkg == "pluto/rt" || pkg == "pluto/alloc" && (name == "Slab" || name == "SyncSlab") || pkg == "pluto/arena" && (name == "Arena" || name == "Fixed" || name == "Chunked") {
			return nil
		}
	}
	iface := l.rtType("Allocator").Underlying().(*types2.Interface)
	if !types2.Implements(T, iface) {
		return nil
	}
	name := compat.Concat(funcSymName((*fn), targs), ".checked")
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	impl := l.rawFuncSym((*fn), targs)
	alloc, _, _ := types2.LookupFieldOrMethod(T, false, nil, "Alloc")
	allocFn := alloc.(*types2.Func)
	key := pu.Alloc[*ssa.Sym]()
	allocRecv := allocFn.Type().(*types2.Signature).Recv().Type()
	if p, ok := allocRecv.(*types2.Pointer); ok {
		allocRecv = p.Elem()
	}
	// A promoted interface Alloc dispatches dynamically. It owns no layer
	// associated with this Free method. Concrete promoted methods use the
	// arguments of their actual receiver, not this method's receiver.
	if _, ok := allocRecv.Underlying().(*types2.Interface); !ok && ((*fn).Name() != "Free" || types2.Identical(base, allocRecv)) {
		var keyArgs []types2.Type
		if n, ok := allocRecv.(*types2.Named); ok {
			for i := 0; i < n.TypeArgs().Len(); i++ {
				keyArgs = compat.Append(keyArgs, n.TypeArgs().At(i))
			}
		}
		(*key) = l.rawFuncSym(allocFn, keyArgs)
	}
	l.symFuncs[sym] = (*fn)
	l.queue = compat.Append(l.queue, pu.New(funcJob{sym: sym, sig: (*sig), build: pu.New(goClosure_genAllocatorSym_1{sig: sig, key: key, fn: fn, impl: impl})}))
	return sym
}

// goClosure_genAllocatorSym_1 is the function literal at genallocator.go:67, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_genAllocatorSym_1 struct {
	sig  **types2.Signature
	key  **ssa.Sym
	fn   **types2.Func
	impl *ssa.Sym
}

func (goSelf *goClosure_genAllocatorSym_1) Call(s *fnState) {
	in := s.incomingArgs((*goSelf.sig))
	keyValue := s.constInt(ssa.TI64, 0)
	if (*goSelf.key) != nil {
		keyValue = s.newValue(ssa.OpBitcast, ssa.TI64, s.symAddr((*goSelf.key)))
	}
	original := in[1].v
	if (*goSelf.fn).Name() == "Free" {
		p := s.callRuntime("GenCustomFree", compat.Clone([]*ssa.Type{ssa.TPtr}), []*ssa.Value{in[1].v, in[2].v, keyValue})[0]
		in[1] = scalarValue(p, in[1].T)
	}
	res := s.emitCall(callee{sym: goSelf.impl}, (*goSelf.sig), &in[0], in[1:], token.NoPos)
	if (*goSelf.fn).Name() == "Free" {
		s.callRuntime("GenAfterCustomFree", nil, []*ssa.Value{original, in[2].v})
	}
	if (*goSelf.fn).Name() == "Alloc" {
		p := s.callRuntime("GenCustomAlloc", compat.Clone([]*ssa.Type{ssa.TPtr}), []*ssa.Value{res[0].v, in[1].v, keyValue})[0]
		res[0] = scalarValue(p, res[0].T)
	}
	s.returnValues(res)
}
