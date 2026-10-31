package lower

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/sort"
)

// Task stacks (language spec section 19.1, item 1; roadmap item 18). A task
// statement passes the runtime the stack its task is proven to need, or 0.
// Lowering cannot know it: the size of every frame is only known once the
// backend has compiled the program. So the task statement loads its bound
// from a read-only word of its own, which the backend fills in once it has
// compiled every function (backend/stack.p), and lowering records what the
// backend needs for that: the task starts, and the functions each indirect
// call may reach, resolved as the context check resolves them
// (context.go).
//
// Only hosted builds that run the runtime's own scheduler get bounds. A
// kernel's or a program's scheduler, and the interrupt frames of a kernel,
// put things on a task's stack that the compiler does not see, so their
// tasks keep the size the scheduler gives tasks of unknown need.

// stackBoundsOff returns why the program's task statements get no computed
// stack bounds, or "".
func (l *Lowerer) stackBoundsOff() string {
	switch {
	case l.cfg.Kernel:
		return "kernel builds do not compute task stacks"
	case l.cfg.Freestanding:
		return "freestanding builds do not compute task stacks"
	case l.sched == nil || l.sched.v.Pkg() == nil || l.sched.v.Pkg().Path() != "pluto/rt":
		return "the program binds its own scheduler, whose stack use the compiler does not know"
	}
	return ""
}

// taskStackBound returns the stack bound argument of the runtime call that
// starts the task statement at pos, whose task runs thunk.
func (s *fnState) taskStackBound(thunk *ssa.Sym, pos token.Pos) *ssa.Value {
	l := s.l
	if l.out.Stack == nil {
		l.out.Stack = pu.New(ssa.StackInfo{Indirect: map[string][]string{}, Open: map[string]bool{}, Off: l.stackBoundsOff()})
	}
	st := l.out.Stack
	if st.Off != "" {
		st.Roots = compat.Append(st.Roots, ssa.StackRoot{Thunk: thunk.Name, Pos: pos})
		return s.constInt(ssa.TI64, 0)
	}
	sym := l.out.Sym(thunk.Name+".stack", ssa.SymData)
	l.out.Data = compat.Append(l.out.Data, pu.New(ssa.Data{Sym: sym, Size: 8, Align: 8, Bytes: compat.Make[byte](8), ReadOnly: true, Local: true}))
	st.Roots = compat.Append(st.Roots, ssa.StackRoot{Thunk: thunk.Name, Bound: sym, Pos: pos})
	return s.load(ssa.TI64, s.symAddr(sym))
}

// resolveIndirectCalls fills in the indirect calls of the program's
// StackInfo: for each function, the functions its calls through func
// values and interfaces may reach, or that it has one that may reach any.
func (l *Lowerer) resolveIndirectCalls() {
	st := l.out.Stack
	if st == nil || st.Off != "" {
		return
	}
	g := &callGraph{l: l, funcs: l.funcsBySym(), edges: map[*ssa.Sym][]hop{}, ifaceTargets: map[string][]*ssa.Sym{}, hooks: l.findHooks()}
	for sym := range l.funcValues {
		if !g.hooks.hidden[sym] {
			g.valueFuncs = compat.Append(g.valueFuncs, sym)
		}
	}
	sort.SortFunc(g.valueFuncs, func(a, b *ssa.Sym) bool { return a.Name < b.Name })
	// Calls through func values of one type have the same candidates:
	// resolve each type once.
	bySig := map[string][]sigTargetSet{}
	for _, f := range l.out.Funcs {
		seen := map[*ssa.Sym]bool{}
		var targets []string
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op != ssa.OpCallInd {
					continue
				}
				var ts []*ssa.Sym
				site := l.sites[v]
				switch {
				case v.Args[0].Op == ssa.OpFuncAddr:
					ts = compat.Clone([]*ssa.Sym{v.Args[0].Aux.(*ssa.Sym)})
				case site == nil:
					st.Open[f.Name] = true
					continue
				case site.fsig != nil && site.lit == nil && site.hook == nil && !site.drop && site.iface == nil:
					ts = g.sigTargets(site, bySig)
				default:
					ts = g.targets(site)
				}
				for _, t := range ts {
					if !seen[t] {
						seen[t] = true
						targets = compat.Append(targets, t.Name)
					}
				}
			}
		}
		if len(targets) > 0 {
			sort.Strings(targets)
			st.Indirect[f.Name] = targets
		}
	}
}

// A sigTargetSet is the candidates of calls through func values of one
// type.
type sigTargetSet struct {
	sig     *types2.Signature
	targets []*ssa.Sym
}

// sigTargets is g.targets for a call through a func value of the type
// site.fsig, cached in bySig by the type's spelling.
func (g *callGraph) sigTargets(site *callSite, bySig map[string][]sigTargetSet) []*ssa.Sym {
	want := plainSig(site.fsig)
	key := typeString(want)
	for _, set := range bySig[key] {
		if types2.Identical(set.sig, want) {
			return set.targets
		}
	}
	ts := g.targets(site)
	bySig[key] = compat.Append(bySig[key], sigTargetSet{sig: want, targets: ts})
	return ts
}
