package opt

import "github.com/matjam/plutonium/internal/ssa"

// ElideStringHolds removes the references lowering takes to keep a
// borrowed string, shared, synced or weak reference, or channel, alive
// while calls run (ssa.StrHold) when none of those calls can release one
// (language spec sections 6.5 and 6.10.1).
//
// A function may release a reference if it calls one of the runtime's release
// functions, makes an indirect call (through a func value or an interface,
// whose target is not known here), or calls a function that may release.
// Functions without a body here (assembly and C) release nothing. A call
// that never returns (a panic) does not count: the borrowed string is not
// used after it.
//
// It must run before inlining, which merges the runtime's calls into the
// functions that make them.
func ElideStringHolds(p *ssa.Program) {
	funcs := map[*ssa.Sym]*ssa.Func{}
	for _, f := range p.Funcs {
		funcs[f.Sym] = f
	}
	may := releasers(p, funcs)
	for _, f := range p.Funcs {
		if len(f.StrHolds) == 0 {
			continue
		}
		var dead []*ssa.Value
		for _, h := range f.StrHolds {
			if holdNeeded(h, funcs, may) {
				continue
			}
			dead = append(dead, h.Retains...)
			dead = append(dead, h.Releases...)
		}
		removeCalls(f, dead)
		f.StrHolds = nil
	}
}

// releaseFuncs are the runtime functions that drop counted references:
// strings, and the shared, synced and weak references and channels whose
// release may free an object holding strings.
var releaseFuncs = map[string]bool{
	"pluto/rt.strRelease":       true,
	"pluto/rt.strFree":          true,
	"pluto/rt.ReleaseString":    true,
	"pluto/rt.boxReleaseStrong": true,
	"pluto/rt.weakRelease":      true,
	"pluto/rt.boxFree":          true,
	"pluto/rt.chanRelease":      true,
	"pluto/rt.chanDrop":         true,
	"pluto/rt.chanFree":         true,
}

// mayReleaseCall reports whether call v may release a string, given what
// is known of the functions so far.
func mayReleaseCall(v *ssa.Value, funcs map[*ssa.Sym]*ssa.Func, may map[*ssa.Func]bool) bool {
	if v.Call != nil && v.Call.NoReturn {
		return false
	}
	switch v.Op {
	case ssa.OpCallInd:
		return true
	case ssa.OpCall:
		sym := v.Aux.(*ssa.Sym)
		if releaseFuncs[sym.Name] {
			return true
		}
		if g := funcs[sym]; g != nil {
			return may[g]
		}
	}
	return false
}

// releasers returns the functions that may release a string, directly or
// through their calls.
func releasers(p *ssa.Program, funcs map[*ssa.Sym]*ssa.Func) map[*ssa.Func]bool {
	may := map[*ssa.Func]bool{}
	callers := map[*ssa.Func][]*ssa.Func{}
	var work []*ssa.Func
	for _, f := range p.Funcs {
		direct := false
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op != ssa.OpCall && v.Op != ssa.OpCallInd {
					continue
				}
				if v.Call != nil && v.Call.NoReturn {
					continue
				}
				if v.Op == ssa.OpCallInd || releaseFuncs[v.Aux.(*ssa.Sym).Name] {
					direct = true
					continue
				}
				if g := funcs[v.Aux.(*ssa.Sym)]; g != nil {
					callers[g] = append(callers[g], f)
				}
			}
		}
		if direct {
			may[f] = true
			work = append(work, f)
		}
	}
	for len(work) > 0 {
		g := work[len(work)-1]
		work = work[:len(work)-1]
		for _, f := range callers[g] {
			if !may[f] {
				may[f] = true
				work = append(work, f)
			}
		}
	}
	return may
}

// holdNeeded reports whether a call covered by h may release a string.
func holdNeeded(h ssa.StrHold, funcs map[*ssa.Sym]*ssa.Func, may map[*ssa.Func]bool) bool {
	for _, c := range h.Covered {
		if mayReleaseCall(c, funcs, may) {
			return true
		}
	}
	return false
}

// removeCalls deletes the calls in dead, which return nothing but memory:
// each use of a call's memory result uses the memory it was given instead.
func removeCalls(f *ssa.Func, dead []*ssa.Value) {
	if len(dead) == 0 {
		return
	}
	gone := map[*ssa.Value]bool{}
	for _, v := range dead {
		gone[v] = true
	}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpSelectN && gone[v.Args[0]] {
				v.CopyOf(v.Args[0].MemArg())
			}
		}
	}
	for _, b := range f.Blocks {
		keep := b.Values[:0]
		for _, v := range b.Values {
			if gone[v] {
				v.ResetArgs()
				continue
			}
			keep = append(keep, v)
		}
		clear(b.Values[len(keep):])
		b.Values = keep
	}
	ssa.CopyElim(f)
}
