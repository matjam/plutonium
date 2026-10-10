package opt

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/ssa"
)

// CSE replaces each value with an equivalent value that dominates it.
// Two values are equivalent when they have the same operation, type,
// auxiliary data and arguments (after replacement). Loads count as pure:
// two loads of the same address from the same memory state read the same
// value. Volatile loads and values with side effects are never merged.
func CSE(f *ssa.Func) {
	dom := f.Dom()
	table := map[string]*ssa.Value{}
	var walk func(b *ssa.Block)
	walk = func(b *ssa.Block) {
		var added []string
		for _, v := range b.Values {
			for i, a := range v.Args {
				if a.Op == ssa.OpCopy {
					for a.Op == ssa.OpCopy {
						a = a.Args[0]
					}
					v.SetArg(i, a)
				}
			}
			if !cseable(v) {
				continue
			}
			k := cseKey(v)
			if w, ok := table[k]; ok {
				v.CopyOf(w)
				continue
			}
			table[k] = v
			added = append(added, k)
		}
		for _, c := range dom.Children(b) {
			walk(c)
		}
		for _, k := range added {
			delete(table, k)
		}
	}
	// Constants live in the entry block and dominate everything: merge them
	// first so later keys see one copy.
	walk(f.Entry)
	ssa.CopyElim(f)
	ssa.DeadCode(f)
}

func cseable(v *ssa.Value) bool {
	switch {
	case v.Op == ssa.OpPhi || v.Op == ssa.OpCopy:
		return false
	case v.Op.NoCSE() || v.Op.HasSideEffect() || v.Op.HasMemResult():
		return false
	case v.IsVolatile():
		return false
	case v.Type.IsMem() || v.Type.IsTuple():
		return false
	case v.Op == ssa.OpSelectN:
		return false
	}
	return true
}

func cseKey(v *ssa.Value) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d|%s|%d|", v.Op, v.Type, v.AuxInt)
	if v.Aux != nil {
		fmt.Fprintf(&sb, "%p|", v.Aux)
	}
	args := v.Args
	if v.Op.IsCommutative() && len(args) == 2 && args[0].ID > args[1].ID {
		args = []*ssa.Value{args[1], args[0]}
	}
	for _, a := range args {
		fmt.Fprintf(&sb, "v%d,", a.ID)
	}
	return sb.String()
}
