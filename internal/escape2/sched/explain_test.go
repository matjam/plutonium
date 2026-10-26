package sched

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// TestExplain is a debugging aid, not a test: it runs the analysis on one
// program and prints what the variables below ask for. It is skipped
// unless ESCAPE2_EXPLAIN names the program's directory (absolute, or
// relative to the repository root), so it changes nothing in a normal run.
// Deferral follows PLUTO_ESCAPE2_DEFER as in the compiler. A large program
// (selfhost/cmd/pluto) needs about 10 GB: run the compiled test binary
// under bigjob.sh with -test.run 'TestExplain$' -test.v.
//
//	ESCAPE2_EXPLAIN_SITES=1       every finding: position, code, function,
//	                              object, and the callee and entry named
//	ESCAPE2_EXPLAIN_CLASSIFY=1    the findings by file, by base (the function
//	                              whose own code makes the entry the finding's
//	                              reason chain ends at; "[fmt]" when the chain
//	                              passes through pluto/fmt's printer) and by
//	                              the first function of the chain
//	ESCAPE2_EXPLAIN_CHAIN=s       the reason chains of the first findings
//	                              (ESCAPE2_EXPLAIN_CHAIN_N, default 4) whose
//	                              base contains s: each step's entry key and
//	                              call site (implies CLASSIFY)
//	ESCAPE2_EXPLAIN_ORIGIN=id     every summary with a Deferred entry of
//	                              origin function id, with its key
//	ESCAPE2_EXPLAIN_SUMS=a|b      the summaries of the functions so named
//	ESCAPE2_EXPLAIN_FOLLOW=f#e    the reason chain of entry e of function f
//	ESCAPE2_EXPLAIN_MEMBERS=f#e   when entry e of f comes from a join, the
//	                              members with that entry and their chains
//	ESCAPE2_EXPLAIN_IDS=1,2       the names of function IDs
//	ESCAPE2_EXPLAIN_FUNC=f        the EIR of function f and the candidates
//	                              of its dynamic calls
//	ESCAPE2_EXPLAIN_STEPS=1       the functions whose solves take the most
//	                              steps
//	ESCAPE2_EXPLAIN_ALLOCDEBUG=1  load as pluto build --alloc=debug does
//	ESCAPE2_EXPLAIN_HEAPONLY=p.T.f  whether field f of type T of package p is
//	                              heap-only (stage B), and if not, the chain of
//	                              requirements to the write that withdrew it
//
// Function names are eir.Func.Name ("selfhost/internal/types2.(*Checker).labels",
// "func literal in ... at file:line:col").
func TestExplain(t *testing.T) {
	dir := os.Getenv("ESCAPE2_EXPLAIN")
	if dir == "" {
		t.Skip("ESCAPE2_EXPLAIN is not set")
	}
	if !filepath.IsAbs(dir) {
		root, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		dir = filepath.Join(root, dir)
	}
	cfg := &load.Config{Tags: map[string]bool{"amd64": true, "linux": true, "hosted": true}, Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	if os.Getenv("ESCAPE2_EXPLAIN_ALLOCDEBUG") != "" {
		// As pluto build --alloc=debug loads (internal/build loadConfig).
		cfg.Tags["allocdebug"] = true
		cfg.Tags["allocsites"] = true
		cfg.Preload = append(cfg.Preload, "pluto/alloc/debug")
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := eir.Build(prog)
	if err != nil {
		t.Fatal(err)
	}
	x := &explainer{t: t, prog: prog, p: p}
	if fn := os.Getenv("ESCAPE2_EXPLAIN_FUNC"); fn != "" {
		x.dumpFunc(fn)
	}
	steps := x.measureSteps(os.Getenv("ESCAPE2_EXPLAIN_STEPS") != "")
	start := time.Now()
	res := RunWith(p, Options{})
	x.e = res.Env.(*env)
	x.res = res
	byCode := map[string]int{}
	for _, f := range res.Findings {
		byCode[string(f.Code)]++
	}
	t.Logf("findings: %d in %v; by code %v", len(res.Findings), time.Since(start), byCode)
	if steps != nil {
		steps()
	}
	if os.Getenv("ESCAPE2_EXPLAIN_SITES") != "" {
		for _, f := range res.Findings {
			obj := ""
			if fn := p.Funcs[f.Func]; int(f.Obj) < len(fn.Objects) {
				obj = fmt.Sprintf("obj %d kind %d %v", f.Obj, fn.Objects[f.Obj].Kind, fn.Objects[f.Obj].Type)
			}
			t.Logf("SITE %s %s %s %s entry %d callee %s", prog.Fset.Position(f.Pos), f.Code, p.Funcs[f.Func].Name, obj, f.Entry, x.name(f.Callee))
		}
	}
	if ids := os.Getenv("ESCAPE2_EXPLAIN_IDS"); ids != "" {
		for _, s := range strings.Split(ids, ",") {
			id, _ := strconv.Atoi(s)
			t.Logf("ID %d = %s", id, x.name(eir.FuncID(id)))
		}
	}
	if og := os.Getenv("ESCAPE2_EXPLAIN_ORIGIN"); og != "" {
		o, _ := strconv.Atoi(og)
		x.origin(eir.FuncID(o))
	}
	if names := os.Getenv("ESCAPE2_EXPLAIN_SUMS"); names != "" {
		x.sums(strings.Split(names, "|"))
	}
	if fe := os.Getenv("ESCAPE2_EXPLAIN_FOLLOW"); fe != "" {
		name, e, _ := strings.Cut(fe, "#")
		entry, _ := strconv.Atoi(e)
		for id, f := range p.Funcs {
			if f.Name == name {
				t.Logf("FOLLOW %s entry %d", name, entry)
				for _, step := range x.chain(eir.FuncID(id), entry) {
					t.Logf("    %s", step)
				}
			}
		}
	}
	if ho := os.Getenv("ESCAPE2_EXPLAIN_HEAPONLY"); ho != "" {
		x.heapOnly(ho)
	}
	if fe := os.Getenv("ESCAPE2_EXPLAIN_MEMBERS"); fe != "" {
		name, e, _ := strings.Cut(fe, "#")
		entry, _ := strconv.Atoi(e)
		for id, f := range p.Funcs {
			if f.Name != name {
				continue
			}
			s := x.sum(eir.FuncID(id))
			r := s.Reasons[entry]
			v := eir.FuncID(r.Callee)
			if !x.e.virt.isVirtual(v) {
				t.Logf("MEMBERS: not virtual")
				continue
			}
			vs := x.sum(v)
			_, a, b, _ := x.entry(vs, int(int32(r.Entry)))
			for _, m := range x.e.virt.sets[int(v)-len(p.Funcs)] {
				ms := x.sum(m)
				for i := 0; ms != nil && i < len(ms.Esc)+len(ms.Store); i++ {
					if _, c, d, _ := x.entry(ms, i); c == a && d == b {
						t.Logf("MEMBER %s entry %d", x.name(m), i)
						for _, step := range x.chain(m, i) {
							t.Logf("      %s", step)
						}
					}
				}
			}
		}
	}
	chain := os.Getenv("ESCAPE2_EXPLAIN_CHAIN")
	if chain != "" || os.Getenv("ESCAPE2_EXPLAIN_CLASSIFY") != "" {
		n, err := strconv.Atoi(os.Getenv("ESCAPE2_EXPLAIN_CHAIN_N"))
		if err != nil {
			n = 4
		}
		x.classify(chain, n)
	}
}

type explainer struct {
	t    *testing.T
	prog *load.Program
	p    *eir.Program
	e    *env
	res  *Result
}

// sum is the final summary of id, a virtual function's join included.
func (x *explainer) sum(id eir.FuncID) *summary.Summary {
	if x.e.virt.isVirtual(id) {
		return x.e.virt.summary(id, func(m eir.FuncID) *summary.Summary { return x.res.Summaries[m] })
	}
	if int(id) < len(x.res.Summaries) {
		return x.res.Summaries[id]
	}
	return nil
}

func (x *explainer) name(id eir.FuncID) string {
	if int(id) < len(x.p.Funcs) {
		return x.p.Funcs[id].Name
	}
	if x.e != nil && x.e.virt.isVirtual(id) {
		return fmt.Sprintf("join of %d candidates", len(x.e.virt.sets[int(id)-len(x.p.Funcs)]))
	}
	return "?"
}

func (x *explainer) keyString(k domain.Key) string {
	s := fmt.Sprintf("in%d%v ge=%v fge=%v", k.Input(), x.e.paths.Steps(k.Path()), k.GE(), k.FieldsGE())
	if k.Deep() {
		s += " deep"
	}
	if id, pos := k.Filter(); id != 0 {
		f, _ := x.e.paths.Filters().Get(id)
		var ts []string
		for _, a := range f.Atoms {
			ts = append(ts, a.Text())
		}
		s += fmt.Sprintf(" filter{%v @%d}", ts, pos)
	}
	return s
}

// entry describes entry i of s (an Esc or a Store entry) and returns its
// keys, to match the entry in a join's members.
func (x *explainer) entry(s *summary.Summary, i int) (string, domain.Key, domain.Key, bool) {
	if i < len(s.Esc) {
		return "esc " + x.keyString(s.Esc[i]), s.Esc[i], 0, true
	}
	if j := i - len(s.Esc); j < len(s.Store) {
		st := s.Store[j]
		return "store " + x.keyString(st.Src) + " -> " + x.keyString(st.Dst), st.Src, st.Dst, true
	}
	return "", 0, 0, false
}

// callPos is the position of the call at site of f.
func (x *explainer) callPos(f eir.FuncID, site uint32) string {
	if int(f) >= len(x.p.Funcs) {
		return "?"
	}
	for _, b := range x.p.Funcs[f].Blocks {
		for _, in := range b.Instrs {
			if in.Op == eir.OpCall && uint32(in.Call.Site) == site {
				return x.prog.Fset.Position(in.Pos).String()
			}
		}
	}
	return "?"
}

// chain follows the reasons from entry e of callee down to the function
// whose own code makes it: one line per step. A join's step goes to the
// member with the same entry.
func (x *explainer) chain(callee eir.FuncID, e int) []string {
	type node struct {
		f eir.FuncID
		e int
	}
	seen := map[node]bool{}
	var out []string
	for n := (node{callee, e}); len(out) < 64 && !seen[n]; {
		seen[n] = true
		s := x.sum(n.f)
		if s == nil {
			out = append(out, x.name(n.f)+": no summary")
			break
		}
		desc, a, b, ok := x.entry(s, n.e)
		if !ok {
			out = append(out, fmt.Sprintf("%s: no entry %d", x.name(n.f), n.e))
			break
		}
		if x.e.virt.isVirtual(n.f) {
			next, found := node{}, false
			if n.e < len(s.Reasons) && s.Reasons[n.e].Callee != ^uint32(0) {
				next, found = node{eir.FuncID(s.Reasons[n.e].Callee), int(s.Reasons[n.e].Entry)}, true
			}
			for _, m := range x.e.virt.sets[int(n.f)-len(x.p.Funcs)] {
				ms := x.sum(m)
				for i := 0; !found && ms != nil && i < len(ms.Esc)+len(ms.Store); i++ {
					if _, c, d, _ := x.entry(ms, i); c == a && d == b {
						next, found = node{m, i}, true
					}
				}
			}
			out = append(out, fmt.Sprintf("%s [%s]", x.name(n.f), desc))
			if !found {
				break
			}
			n = next
			continue
		}
		if n.e >= len(s.Reasons) {
			out = append(out, fmt.Sprintf("%s [entry %d: %s]: no reason", x.name(n.f), n.e, desc))
			break
		}
		r := s.Reasons[n.e]
		if r.Callee == ^uint32(0) {
			out = append(out, fmt.Sprintf("%s [entry %d: %s]: its own code at %s", x.name(n.f), n.e, desc, x.prog.Fset.Position(token.Pos(r.Pos))))
			break
		}
		out = append(out, fmt.Sprintf("%s [entry %d: %s] via call at %s", x.name(n.f), n.e, desc, x.callPos(n.f, r.Site)))
		n = node{eir.FuncID(r.Callee), int(int32(r.Entry))}
	}
	return out
}

// classify prints the findings by file, base and first function, and the
// chains of the first n findings whose base contains want.
func (x *explainer) classify(want string, n int) {
	byBase, byFile, byFirst := map[string]int{}, map[string]int{}, map[string]int{}
	samples := map[string]string{}
	for _, fd := range x.res.Findings {
		pos := x.prog.Fset.Position(fd.Pos)
		file := pos.Filename
		if i := strings.Index(file, "selfhost/"); i >= 0 {
			file = file[i:]
		}
		byFile[filepath.Dir(file)]++
		base, first := "own code of the function", ""
		var steps []string
		if fd.Callee != eir.NoFunc {
			steps = x.chain(fd.Callee, fd.Entry)
			last := steps[len(steps)-1]
			base = "end: " + last
			if i := strings.Index(last, " ["); i >= 0 {
				base = "own:" + last[:i]
				if !strings.Contains(last, "its own code") {
					base = "unfounded:" + last[:i]
				}
			}
			if i := strings.Index(steps[0], " ["); i >= 0 {
				first = steps[0][:i]
			}
			for _, s := range steps {
				if strings.HasPrefix(s, "pluto/fmt.(*printer).") {
					base = "[fmt] " + base
					break
				}
			}
		}
		if i := strings.Index(base, " at /"); i >= 0 {
			base = base[:i]
		}
		byBase[base]++
		byFirst[first]++
		if _, ok := samples[base]; !ok {
			samples[base] = fmt.Sprintf("%s %s in %s", pos, fd.Code, x.p.Funcs[fd.Func].Name)
		}
		if want != "" && n > 0 && strings.Contains(base, want) {
			n--
			x.t.Logf("CHAIN %s %s in %s", pos, fd.Code, x.p.Funcs[fd.Func].Name)
			for _, s := range steps {
				x.t.Logf("    %s", s)
			}
		}
	}
	top := func(label string, m map[string]int, k int) {
		type kv struct {
			k string
			v int
		}
		var out []kv
		for key, v := range m {
			out = append(out, kv{key, v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].v > out[j].v || out[i].v == out[j].v && out[i].k < out[j].k })
		for i, e := range out {
			if i == k {
				break
			}
			if label == "BASE" {
				x.t.Logf("%s %6d %s  e.g. %s", label, e.v, e.k, samples[e.k])
			} else {
				x.t.Logf("%s %6d %s", label, e.v, e.k)
			}
		}
	}
	top("FILE", byFile, 25)
	top("BASE", byBase, 40)
	top("FIRST", byFirst, 25)
}

// origin prints every summary with a Deferred entry of origin function o.
func (x *explainer) origin(o eir.FuncID) {
	x.t.Logf("ORIGIN %d = %s", o, x.name(o))
	for id, f := range x.p.Funcs {
		s := x.res.Summaries[id]
		if s == nil {
			continue
		}
		for _, d := range s.Deferred {
			if d.Origin == o {
				x.t.Logf("ORIGIN in %s: fn %s site %d, %d args (%d Deferred entries)", f.Name, x.keyString(d.Fn), d.Site, len(d.Args), len(s.Deferred))
			}
		}
	}
}

// sums prints the summaries of the functions with the given names.
func (x *explainer) sums(names []string) {
	for id, f := range x.p.Funcs {
		if !contains(names, f.Name) {
			continue
		}
		s := x.res.Summaries[id]
		if s == nil {
			x.t.Logf("SUM %s: none", f.Name)
			continue
		}
		x.t.Logf("SUM %s: %d inputs", f.Name, s.Inputs)
		for i := 0; i < len(s.Esc)+len(s.Store); i++ {
			desc, _, _, _ := x.entry(s, i)
			x.t.Logf("  %d %s", i, desc)
		}
		for _, k := range s.HeapStore {
			x.t.Logf("  heapstore %s", x.keyString(k))
		}
		for r, res := range s.Res {
			for _, rk := range res.From {
				x.t.Logf("  res %d from %s field %v fieldGE %v", r, x.keyString(rk.Key), x.e.paths.Steps(rk.Field), rk.FieldGE)
			}
			x.t.Logf("  res %d heap %d counted %d", r, len(res.Heap), len(res.CountedHeap))
		}
		for _, d := range s.Deferred {
			x.t.Logf("  deferred fn %s origin %d site %d, %d args", x.keyString(d.Fn), d.Origin, d.Site, len(d.Args))
			for i, a := range d.Args {
				var ks []string
				for _, k := range a.Keys {
					ks = append(ks, x.keyString(k))
				}
				x.t.Logf("    arg %d heap=%v %v", i, a.Heap, ks)
			}
		}
		x.t.Logf("  region builds=%v outer=%v lits=%v self=%v", s.Region.Builds, s.Region.OuterResults, s.Region.Lits, s.Region.Self)
	}
}

// dumpFunc prints the EIR of the function named fn and the candidates of
// its dynamic calls.
func (x *explainer) dumpFunc(fn string) {
	fs := stageB(x.p)
	for _, f := range x.p.Funcs {
		if f.Name != fn {
			continue
		}
		var sb strings.Builder
		x.p.Dump(&sb, x.prog.Fset, f)
		x.t.Logf("DUMP %s\n%s", f.Name, sb.String())
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if in.Op == eir.OpCall && in.Call.Kind != eir.CallStatic && in.Call.Kind != eir.CallContract {
					var names []string
					for _, id := range fs.Candidates(f, in.Call) {
						names = append(names, x.name(id))
					}
					x.t.Logf("DYN at %s kind %d: %v", x.prog.Fset.Position(in.Pos), in.Call.Kind, names)
				}
			}
		}
	}
}

// measureSteps records the solve steps by function if on, and returns
// what prints them.
func (x *explainer) measureSteps(on bool) func() {
	if !on {
		return nil
	}
	type agg struct{ n, steps, maxSteps, limited int }
	var mu sync.Mutex
	by := map[string]*agg{}
	solve.Measure = func(f *eir.Func, m solve.Measurement) {
		mu.Lock()
		defer mu.Unlock()
		a := by[f.Name]
		if a == nil {
			a = &agg{}
			by[f.Name] = a
		}
		a.n++
		a.steps += m.Steps
		a.maxSteps = max(a.maxSteps, m.Steps)
		if m.Limited {
			a.limited++
		}
	}
	return func() {
		solve.Measure = nil
		names := make([]string, 0, len(by))
		total := 0
		for k, a := range by {
			names = append(names, k)
			total += a.steps
		}
		sort.Slice(names, func(i, j int) bool { return by[names[i]].steps > by[names[j]].steps })
		x.t.Logf("total steps %d", total)
		for i, k := range names {
			if i == 25 {
				break
			}
			a := by[k]
			x.t.Logf("STEPS %12d in %5d solves, max %10d, limited %d: %s", a.steps, a.n, a.maxSteps, a.limited, k)
		}
	}
}

// heapOnly prints whether the field named by spec (pkgpath.Type.field) is
// heap-only, and why not.
func (x *explainer) heapOnly(spec string) {
	i := strings.LastIndex(spec, ".")
	j := strings.LastIndex(spec[:max(i, 0)], ".")
	if i < 0 || j < 0 {
		x.t.Logf("HEAPONLY %s: want pkgpath.Type.field", spec)
		return
	}
	pkgPath, typeName, field := spec[:j], spec[j+1:i], spec[i+1:]
	for _, pkg := range x.prog.Packages {
		if pkg.Path != pkgPath || pkg.Types == nil {
			continue
		}
		tn, ok := pkg.Types.Scope().Lookup(typeName).(*types2.TypeName)
		if !ok {
			continue
		}
		st, ok := tn.Type().Underlying().(*types2.Struct)
		if !ok {
			continue
		}
		for k := 0; k < st.NumFields(); k++ {
			if st.Field(k).Name() != field {
				continue
			}
			x.t.Logf("HEAPONLY %s: %v", spec, x.e.facts.HeapOnly(tn.Type(), k))
			for _, l := range x.e.facts.ho.explain(tn.Type(), k) {
				x.t.Logf("    %s", l)
			}
			return
		}
	}
	x.t.Logf("HEAPONLY %s: no such field", spec)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
