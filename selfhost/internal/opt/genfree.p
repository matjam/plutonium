// Translated by go2pu from internal/opt/genfree.go at the go-cutover tag; edited by hand since.

package opt

import (
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/ssa"
)

// GenFrees finds the functions whose calls cannot make a pointer that
// passed a generation check stale (opt.GenCheck, abi.md section 1.3), and
// gives every checked function of the program the result
// (ssa.Func.GenNoFree and GenNoReset).
//
// A check stays valid until the current task could have caused a free or
// observed one made by another task. Memory becomes stale only where a
// granule's generation or epoch changes: in genFreeFuncs, which the slab
// and arena call to free. Another task's free is seen only through
// synchronization: atomics, locks, channels and the scheduler, all of
// which are assembly or reach assembly, intrinsics or indirect calls; a
// free that races with an access, unsynchronized, was never caught
// reliably, since no check is atomic with the access it guards.
//
// So a function may free, and its calls end what was checked, if it is
// one of genFreeFuncs or has code inlined from one, makes an indirect call
// (its target is not known here), calls a function without a body here
// (assembly or C: atomics, system calls, the scheduler's switches, code
// that may call back into Plutonium) other than genQuietAsm, uses an
// intrinsic or a volatile access, or calls a function that may free. A
// call that never returns, and any code from which the function cannot
// return (a panic's path), does not count: nothing after it uses what
// was checked.
//
// A function may reset if it may make stale memory it was not given a
// pointer to: the same, except that the slab's free and calls to an
// Allocator's Alloc and Free do not count (they free only what they are
// passed), and an arena's bulk release (genResetFuncs) does. GenCheck
// keeps a check of an allocation the function holds and has not let
// escape across calls that may free but not reset.
func GenFrees(p *ssa.Program) {
	checked := false
	for _, f := range p.Funcs {
		if f.Pragmas&ssa.PragmaGenCheck != 0 {
			checked = true
			break
		}
	}
	if !checked {
		return
	}
	funcs := map[*ssa.Sym]*ssa.Func{}
	for _, f := range p.Funcs {
		funcs[f.Sym] = f
	}
	noFree := genSafeFuncs(p, funcs, false)
	noReset := genSafeFuncs(p, funcs, true)
	for _, f := range p.Funcs {
		if f.Pragmas&ssa.PragmaGenCheck != 0 {
			f.GenNoFree = noFree
			f.GenNoReset = noReset
		}
	}
}

// genSafeFuncs returns the functions that cannot free (or, with reset,
// cannot reset), directly or through their calls.
func genSafeFuncs(p *ssa.Program, funcs map[*ssa.Sym]*ssa.Func, reset bool) map[*ssa.Sym]bool {
	unsafe := map[*ssa.Func]bool{}
	callers := map[*ssa.Func][]*ssa.Func{}
	var work []*ssa.Func
	for _, f := range p.Funcs {
		if genDirect(f, funcs, callers, reset) {
			unsafe[f] = true
			work = compat.Append(work, f)
		}
	}
	for len(work) > 0 {
		g := work[len(work)-1]
		work = work[:len(work)-1]
		for _, f := range callers[g] {
			if !unsafe[f] {
				unsafe[f] = true
				work = compat.Append(work, f)
			}
		}
	}
	safe := map[*ssa.Sym]bool{}
	for _, f := range p.Funcs {
		if !unsafe[f] {
			safe[f.Sym] = true
		}
	}
	return safe
}

// genFreeFuncs are the functions that change the generation or epoch of
// memory, making pointers to it stale: the slab's free
// (lib/pluto/alloc/gen.p) and the arena's Reset, Release, Drop and Free
// (lib/pluto/arena/epoch.p). Their code may be inlined into other
// functions, so a store inlined from them counts as a free too.
var genFreeFuncs = map[string]bool{
	"pluto/rt.GenInvalidate":          true,
	"pluto/rt.GenUnregister":          true,
	"pluto/rt.GenFreeHeap":            true,
	"pluto/rt.GenCustomFree":          true,
	"pluto/rt.genCustomFreeLocked":    true,
	"pluto/rt.genUnregisterLocked":    true,
	"pluto/rt.GenAfterCustomFree":     true,
	"pluto/alloc.genRelease":          true,
	"pluto/arena.(*Chunked).endEpoch": true,
}

// genResetFuncs take back every allocation of an arena past a point,
// whoever holds it (lib/pluto/arena/chunked.p: Reset, Release and Drop).
var genResetFuncs = map[string]bool{
	"pluto/arena.(*Chunked).release": true,
}

// genQuietAsm are the runtime's assembly functions that neither free nor
// synchronize with another task: they read the thread's registers, or
// count what only the running task reads (lib/pluto/rt/*_amd64.s).
var genQuietAsm = map[string]bool{
	"pluto/rt.getg":           true,
	"pluto/rt.getm":           true,
	"pluto/rt.getsp":          true,
	"pluto/rt.inc32":          true,
	"pluto/rt.dec32":          true,
	"pluto/rt.callerFrame":    true,
	"pluto/rt.traceTable":     true,
	"pluto/rt.goStartPC":      true,
	"pluto/rt.asyncPreemptPC": true,
}

// genDirect reports whether f may free (or reset) through its own code,
// and records in callers the functions f calls that have a body.
func genDirect(f *ssa.Func, funcs map[*ssa.Sym]*ssa.Func, callers map[*ssa.Func][]*ssa.Func, reset bool) bool {
	prims := genFreeFuncs
	if reset {
		prims = genResetFuncs
	}
	if prims[f.Name] {
		return true
	}
	inlined := genInlinedFrom(f, prims)
	returns := genReturning(f)
	direct := false
	for _, b := range f.Blocks {
		if !returns[b.ID] {
			continue // ends in a panic: f's caller never resumes
		}
		for _, v := range b.Values {
			if v.Op == ssa.OpCall {
				if v.Call != nil && v.Call.NoReturn {
					continue
				}
				sym := v.Aux.(*ssa.Sym)
				g := funcs[sym]
				if g == nil {
					if !genQuietAsm[sym.Name] {
						direct = true // no body here
					}
				} else {
					callers[g] = compat.Append(callers[g], f)
				}
				continue
			}
			if genBarrierOp(v, inlined, reset) {
				direct = true
			}
		}
	}
	return direct
}

// genReturning returns, by block ID, whether f can return from the block.
func genReturning(f *ssa.Func) []bool {
	r := compat.Make[bool](f.NumBlocks())
	var work []*ssa.Block
	for _, b := range f.Blocks {
		if b.Kind == ssa.BlockRet {
			r[b.ID] = true
			work = compat.Append(work, b)
		}
	}
	for len(work) > 0 {
		b := work[len(work)-1]
		work = work[:len(work)-1]
		for _, e := range b.Preds {
			if !r[e.B.ID] {
				r[e.B.ID] = true
				work = compat.Append(work, e.B)
			}
		}
	}
	return r
}

// genBarrierOp reports whether v, other than a direct call, may make a
// checked pointer stale or observe another task's free (with reset:
// memory it was not given a pointer to). inlined says, by inline site,
// which code came from the primitives that do.
func genBarrierOp(v *ssa.Value, inlined []bool, reset bool) bool {
	switch v.Op {
	case ssa.OpCallInd:
		if v.Call != nil && (v.Call.NoReturn || reset && v.Call.Alloc != ssa.AllocNone) {
			return false
		}
		return true
	case ssa.OpIntrinsic, ssa.OpVolatileRead:
		return true
	case ssa.OpLoad, ssa.OpStore:
		if v.IsVolatile() {
			return true
		}
	}
	return v.Inl != 0 && inlined[v.Inl] && v.Op.HasMemResult()
}

// genInlinedFrom returns, by 1-based inline site, whether the site's code
// came from one of prims, directly or through further inlining.
func genInlinedFrom(f *ssa.Func, prims map[string]bool) []bool {
	r := compat.Make[bool](len(f.Inlines) + 1)
	for i := range f.Inlines {
		for s := int32(i + 1); s != 0; s = f.Inlines[s-1].Parent {
			if prims[f.Inlines[s-1].Func] {
				r[i+1] = true
				break
			}
		}
	}
	return r
}

// genFreshFuncs return a new allocation that nothing else refers to yet
// (lib/pluto/alloc/alloc.p, lib/pluto/rt/box.p). The names are a prefix,
// for instances of generic functions.
var genFreshFuncs = []string{
	"pluto/rt.NewBox",
	"pluto/alloc.New[",
	"pluto/alloc.Raw",
	"pluto/alloc.must",
}

// genFreshCall reports whether call v returns, as its first result, a new
// allocation: an Allocator's Alloc, or one of genFreshFuncs.
func genFreshCall(v *ssa.Value) bool {
	switch v.Op {
	case ssa.OpCallInd:
		return v.Call != nil && v.Call.Alloc == ssa.AllocAlloc
	case ssa.OpCall:
		name := v.Aux.(*ssa.Sym).Name
		for _, p := range genFreshFuncs {
			if name == p || strings.HasSuffix(p, "[") && strings.HasPrefix(name, p) {
				return true
			}
		}
	}
	return false
}

// genIsPanicBlock reports whether b only calls one of the runtime's panic
// functions (pluto/rt.panicIndex and the like, and panic itself).
func genIsPanicBlock(b *ssa.Block) bool {
	if b.Kind != ssa.BlockExit || len(b.Controls) != 1 {
		return false
	}
	m := b.Controls[0]
	if m.Op != ssa.OpSelectN || m.Args[0].Op != ssa.OpCall || m.Args[0].Block != b {
		return false
	}
	call := m.Args[0]
	if call.Call == nil || !call.Call.NoReturn || !strings.HasPrefix(call.Aux.(*ssa.Sym).Name, "pluto/rt.panic") {
		return false
	}
	for _, v := range b.Values {
		if v != call && (v.Op.IsCall() || v.Op.HasMemResult()) {
			return false
		}
	}
	return true
}
