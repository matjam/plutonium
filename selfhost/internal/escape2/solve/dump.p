// Translated by go2pu from internal/escape2/solve/dump.go at the go-cutover tag; edited by hand since.

package solve

import (
	"pluto/fmt"
	"pluto/os"
	"pluto/sort"
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/escape2/domain"
	"selfhost/internal/escape2/eir"
	"selfhost/internal/escape2/summary"
	"selfhost/pu"
)

// ESCAPE2_DUMP=<substring>[|<substring>...] prints, for every solve of a
// function whose name contains one of the substrings, what the solve holds and what it read: the
// tokens by kind and by input (count, distinct paths, distinct tags, the
// longest paths), the summaries of its callees (entries per kind), and the
// summary it produces. A solve stopped by its limits is dumped before it
// panics. It is for diagnosing a solve's growth, never for the compiler.
var dumpFilter = compat.Getenv("ESCAPE2_DUMP")

// dumpPass counts the solves of each dumped function: the passes of its SCC.
var dumpPass = map[string]int{}

// dumpMu serializes dumps: solves run concurrently.
var dumpMu compat.Mutex

func (s *solver) wantDump() bool {
	if dumpFilter == "" {
		return false
	}
	for _, f := range compat.Split(dumpFilter, "|") {
		if f != "" && strings.Contains(s.f.Name, f) {
			return true
		}
	}
	return false
}

// dump prints the state of the solve (rules above); res is nil for a solve
// stopped at its limits.
func (s *solver) dump(res *summary.Summary) {
	if !s.wantDump() {
		return
	}
	dumpMu.Lock()
	defer dumpMu.Unlock()
	dumpPass[s.f.Name]++
	b := pu.Alloc[compat.Builder]()
	compat.Fprintfb(b, "== %s pass %d: %d nodes, %d tokens, %d steps, %d bytes\n", compat.ArgS(s.f.Name), compat.ArgI(int64(dumpPass[s.f.Name])), compat.ArgI(int64(len(s.nodeRep.s))), compat.ArgI(int64(len(s.toks.s)/2)), compat.ArgI(int64(s.nsteps)), compat.ArgI(int64(s.a.Allocated()-s.base)))
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
				st = pu.New(inStat{paths: map[domain.PathID]int{}, tags: map[domain.PathID]int{}})
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
	compat.Fprintfb(b, "  tokens by kind: Obj %d, In %d, Heap %d, CHeap %d\n", compat.ArgI(int64(kinds[tObj])), compat.ArgI(int64(kinds[tIn])), compat.ArgI(int64(kinds[tHeap])), compat.ArgI(int64(kinds[tCHeap])))
	var inputs []uint32
	for i := range ins {
		inputs = compat.AppendPlain(inputs, i)
	}
	sort.SortFunc(inputs, func(a, b uint32) bool { return ins[a].toks > ins[b].toks })
	for _, i := range inputs {
		st := ins[i]
		name := ""
		if int(i) < len(s.f.Inputs) && s.f.Inputs[i].Var != nil {
			name = s.f.Inputs[i].Var.Name()
		}
		compat.Fprintfb(b, "  input %d %s: %d tokens, %d paths (%d GE tokens), %d tags\n", compat.ArgU(uint64(i)), compat.ArgS(name), compat.ArgI(int64(st.toks)), compat.ArgI(int64(len(st.paths))), compat.ArgI(int64(st.ge)), compat.ArgI(int64(len(st.tags))))
		// The shapes: paths by number of loads and steps.
		shapes := map[string]int{}
		for p := range st.paths {
			shapes[compat.Sprintfb("%d loads %d steps", compat.ArgI(int64(s.ps.Loads(p))), compat.ArgI(int64(len(s.ps.Steps(p)))))]++
		}
		fmt.Fprintf(b, "    shapes %v\n", shapes)
		var ps []domain.PathID
		for p := range st.paths {
			ps = compat.AppendPlain(ps, p)
		}
		sort.SortFunc(ps, func(a, b domain.PathID) bool { return st.paths[a] > st.paths[b] })
		for k := 0; k < 5 && k < len(ps); k++ {
			fmt.Fprintf(b, "    path %v: %d tokens\n", s.ps.Steps(ps[k]), st.paths[ps[k]])
		}
	}
	compat.Fprintfb(b, "  frame objects with tokens: %d\n", compat.ArgI(int64(len(objs))))
	compat.Fprintfb(b, "  dcalls %d, applications %d\n", compat.ArgI(int64(len(s.dcalls))), compat.ArgI(int64(len(s.applied))))
	for i, dc := range s.dcalls {
		if i == 12 {
			break
		}
		var sizes []int
		for _, a := range dc.args {
			sizes = compat.AppendPlain(sizes, len(s.pts(s.find(a))))
		}
		fmt.Fprintf(b, "    dcall %d origin %d site %d: target %d tokens, args %v, keys %d, fellBack %v argsBad %v kept %v\n", i, dc.origin, dc.site, len(s.pts(s.find(dc.target))), sizes, len(dc.keys), dc.fellBack, dc.argsBad, dc.kept)
	}
	apps := map[eir.FuncID]int{}
	for _, ap := range s.applied {
		apps[ap.cand]++
	}
	type ac struct {
		id eir.FuncID
		n  int
	}
	var acs []ac
	for id, n := range apps {
		acs = compat.Append(acs, ac{id, n})
	}
	sort.SortFunc(acs, func(a, b ac) bool { return a.n > b.n })
	for i, a := range acs {
		if i == 8 {
			break
		}
		name := compat.Sprintfb("virtual %d", compat.ArgU(uint64(a.id)))
		if int(a.id) < len(s.prog.Funcs) {
			name = s.prog.Funcs[a.id].Name
		}
		compat.Fprintfb(b, "    applied %d times: %s\n", compat.ArgI(int64(a.n)), compat.ArgS(name))
	}
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
				name := compat.Sprintfb("virtual %d", compat.ArgU(uint64(id)))
				if int(id) < len(s.prog.Funcs) {
					name = s.prog.Funcs[id].Name
				}
				n := 0
				for _, r := range sum.Res {
					n += len(r.From)
				}
				callees = compat.Append(callees, cs{name, len(sum.Esc), len(sum.Store), len(sum.HeapStore), n, sum.Inputs})
			}
		}
	}
	sort.SortFunc(callees, func(a, b cs) bool {
		return a.esc+a.store+a.res > b.esc+b.store+b.res
	})
	compat.Fprintfb(b, "  %d callees with summaries; the widest:\n", compat.ArgI(int64(len(callees))))
	for k := 0; k < 8 && k < len(callees); k++ {
		c := callees[k]
		compat.Fprintfb(b, "    %s: %d inputs, esc %d, store %d, heapstore %d, res %d\n", compat.ArgS(c.name), compat.ArgI(int64(c.inputs)), compat.ArgI(int64(c.esc)), compat.ArgI(int64(c.store)), compat.ArgI(int64(c.hs)), compat.ArgI(int64(c.res)))
	}
	if res != nil {
		n := 0
		for _, r := range res.Res {
			n += len(r.From)
		}
		compat.Fprintfb(b, "  summary: esc %d, store %d, heapstore %d, res %d\n", compat.ArgI(int64(len(res.Esc))), compat.ArgI(int64(len(res.Store))), compat.ArgI(int64(len(res.HeapStore))), compat.ArgI(int64(n)))
		ks := func(k domain.Key) string {
			f := ""
			switch {
			case k.GE():
				f = "+"
			case k.FieldsGE():
				f = "~"
			case k.Deep():
				f = "*"
			}
			return compat.Sprintf("in%d%v%s", k.Input(), s.ps.Steps(k.Path()), f)
		}
		for k, p := range res.Store {
			if k == 16 {
				break
			}
			compat.Fprintfb(b, "    store %s -> %s\n", compat.ArgS(ks(p.Src)), compat.ArgS(ks(p.Dst)))
		}
		for k, e := range res.Esc {
			if k == 16 {
				break
			}
			why := ""
			if k < len(res.Reasons) {
				r := res.Reasons[k]
				why = compat.Sprintfb(" (site %d, callee %d, entry %d)", compat.ArgI(int64(int32(r.Site))), compat.ArgI(int64(int32(r.Callee))), compat.ArgI(int64(int32(r.Entry))))
			}
			compat.Fprintfb(b, "    esc %s%s\n", compat.ArgS(ks(e)), compat.ArgS(why))
		}
	} else {
		fmt.Fprintf(b, "  stopped at its limits\n")
	}
	compat.Fprintfb(os.Stderr, "%v", compat.ArgS(b.String()))
}
