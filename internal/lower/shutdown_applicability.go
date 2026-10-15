package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/types2"
)

// faultPanic is the runtime's signal-to-fault reporting record. Its normal
// state is empty; sigpanic fills it only before the abrupt raiseFault path.
// It remains available with the other fault-reporting facilities throughout
// package cleanup, rather than creating an application cleanup obligation.
func runtimeFaultStorage(v *types2.Var) bool {
	if v.Pkg() == nil || v.Pkg().Path() != "pluto/rt" || v.Name() != "faultPanic" {
		return false
	}
	t, ok := v.Type().(*types2.Named)
	return ok && t.Obj().Pkg() == v.Pkg() && t.Obj().Name() == "Panic"
}

// No completed static place exists when the final typed inventory is empty.
// Remove the provisional hooks before optimization and do not lower their
// implementation graph. Memory results forward the incoming memory; progress
// queries are false/zero. Actual task draining is independent of this decision.
func (l *Lowerer) elideStaticCleanup() {
	for _, f := range l.out.Funcs {
		calls := map[*ssa.Value]*ssa.Value{}
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if sym, ok := v.Aux.(*ssa.Sym); ok {
					if v.Op == ssa.OpCall && staticCleanupHook(l.symFuncs[sym]) {
						calls[v] = v.MemArg()
					}
				}
			}
		}
		var selects []*ssa.Value
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op == ssa.OpSelectN {
					selects = append(selects, v)
				}
			}
		}
		for _, v := range selects {
			if mem := calls[v.Args[0]]; mem != nil {
				if v.AuxInt == 0 {
					v.CopyOf(mem)
				} else {
					v.CopyOf(f.ConstInt(v.Type, 0))
				}
			}
		}
		for call, mem := range calls {
			call.CopyOf(mem)
			call.Type = ssa.TMem
		}
	}
}
