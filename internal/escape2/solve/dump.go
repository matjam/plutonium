package solve

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// ESCAPE2_DUMP=<substring> prints, for every solve of a function whose name
// contains the substring, what the solve holds and what it read: the
// tokens by kind and by input (count, distinct paths, distinct tags, the
// longest paths), the summaries of its callees (entries per kind), and the
// summary it produces. A solve stopped by its limits is dumped before it
// panics. It is for diagnosing a solve's growth, never for the compiler.
var dumpFilter = os.Getenv("ESCAPE2_DUMP")

// dumpPass counts the solves of each dumped function: the passes of its SCC.
var dumpPass = map[string]int{}

func (s *solver) wantDump() bool {
	return dumpFilter != "" && strings.Contains(s.f.Name, dumpFilter)
}

// dump prints the state of the solve (rules above); res is nil for a solve
// stopped at its limits.
func (s *solver) dump(res *summary.Summary) {
	if !s.wantDump() {
		return
	}
	dumpPass[s.f.Name]++
	var b strings.Builder
	fmt.Fprintf(&b, "== %s pass %d: %d nodes, %d tokens, %d steps, %d bytes\n", s.f.Name, dumpPass[s.f.Name], len(s.nodeRep.s), len(s.toks.s)/2, s.nsteps, s.a.Bytes()-s.base)
	type inStat struct {
		toks        int
		paths, tags map[domain.PathID]int
		ge          int
	}
	ins := map[uint32]*inStat{}
	kinds := map[tokKind]int{}
	objs := map[uint32]int{}
	for i := 0; i < len(s.toks.s)/2; i++ {
		t := s.tokAt(uint64(i))
		kinds[t.kind]++
		switch t.kind {
		case tIn:
			st := ins[t.base]
			if st == nil {
				st = &inStat{paths: map[domain.PathID]int{}, tags: map[domain.PathID]int{}}
				ins[t.base] = st
			}
			st.toks++
			st.paths[t.path]++
			st.tags[t.tag]++
			if t.ge {
				st.ge++
			}
		case tObj:
			objs[t.base]++
		}
	}
	fmt.Fprintf(&b, "  tokens by kind: Obj %d, In %d, Heap %d, CHeap %d\n", kinds[tObj], kinds[tIn], kinds[tHeap], kinds[tCHeap])
	var inputs []uint32
	for i := range ins {
		inputs = append(inputs, i)
	}
	sort.Slice(inputs, func(i, j int) bool { return ins[inputs[i]].toks > ins[inputs[j]].toks })
	for _, i := range inputs {
		st := ins[i]
		name := ""
		if int(i) < len(s.f.Inputs) && s.f.Inputs[i].Var != nil {
			name = s.f.Inputs[i].Var.Name()
		}
		fmt.Fprintf(&b, "  input %d %s: %d tokens, %d paths (%d GE tokens), %d tags\n", i, name, st.toks, len(st.paths), st.ge, len(st.tags))
		// The shapes: paths by number of loads and steps.
		shapes := map[string]int{}
		for p := range st.paths {
			shapes[fmt.Sprintf("%d loads %d steps", s.ps.Loads(p), len(s.ps.Steps(p)))]++
		}
		fmt.Fprintf(&b, "    shapes %v\n", shapes)
		var ps []domain.PathID
		for p := range st.paths {
			ps = append(ps, p)
		}
		sort.Slice(ps, func(a, c int) bool { return st.paths[ps[a]] > st.paths[ps[c]] })
		for k := 0; k < 5 && k < len(ps); k++ {
			fmt.Fprintf(&b, "    path %v: %d tokens\n", s.ps.Steps(ps[k]), st.paths[ps[k]])
		}
	}
	fmt.Fprintf(&b, "  frame objects with tokens: %d\n", len(objs))
	// The callees' summaries this solve applied.
	seen := map[eir.FuncID]bool{}
	type cs struct {
		name                        string
		esc, store, hs, res, inputs int
	}
	var callees []cs
	for _, blk := range s.f.Blocks {
		for _, in := range blk.Instrs {
			if in.Op != eir.OpCall || in.Call == nil {
				continue
			}
			var ids []eir.FuncID
			if in.Call.Kind == eir.CallStatic {
				ids = []eir.FuncID{in.Call.Callee}
			} else if in.Call.Kind != eir.CallContract {
				ids = s.env.Candidates(s.f, in.Call)
			}
			for _, id := range ids {
				if id == eir.NoFunc || seen[id] {
					continue
				}
				seen[id] = true
				sum := s.env.Summary(id)
				if sum == nil {
					continue
				}
				name := fmt.Sprintf("virtual %d", id)
				if int(id) < len(s.prog.Funcs) {
					name = s.prog.Funcs[id].Name
				}
				n := 0
				for _, r := range sum.Res {
					n += len(r.From)
				}
				callees = append(callees, cs{name, len(sum.Esc), len(sum.Store), len(sum.HeapStore), n, sum.Inputs})
			}
		}
	}
	sort.Slice(callees, func(i, j int) bool {
		return callees[i].esc+callees[i].store+callees[i].res > callees[j].esc+callees[j].store+callees[j].res
	})
	fmt.Fprintf(&b, "  %d callees with summaries; the widest:\n", len(callees))
	for k := 0; k < 8 && k < len(callees); k++ {
		c := callees[k]
		fmt.Fprintf(&b, "    %s: %d inputs, esc %d, store %d, heapstore %d, res %d\n", c.name, c.inputs, c.esc, c.store, c.hs, c.res)
	}
	if res != nil {
		n := 0
		for _, r := range res.Res {
			n += len(r.From)
		}
		fmt.Fprintf(&b, "  summary: esc %d, store %d, heapstore %d, res %d\n", len(res.Esc), len(res.Store), len(res.HeapStore), n)
	} else {
		fmt.Fprintf(&b, "  stopped at its limits\n")
	}
	fmt.Fprint(os.Stderr, b.String())
}
