// Command escapescc measures the strongly connected components of a program's
// call graph, which the redesigned escape analysis schedules bottom up (plan
// docs/plans/2026-10-20-escape-redesign.md, sections (b), (e) and (h)): the
// largest SCC bounds the analysis's working memory, the plan's main memory
// risk.
//
//	go run ./tools/escapescc ./selfhost/demo/diagdemo
//
// The graph is the lowered program's (before optimisation), with the
// instances of a generic function merged into one node, as the analysis has
// one body per generic function, and with the functions the lowerer
// synthesizes (wrappers, adapters, thunks, drop glue) seen through, as calls
// of the source functions they reach. It is measured three ways:
//
//   - static: calls of a known function, and the literals and functions a
//     function takes the address of (it builds their closures). Under the
//     provisional rule for dynamic calls (decision D47) this is the whole
//     scheduling graph: a dynamic call applies a fixed summary per signature;
//   - static-contract: static without the calls the compiler adds into the
//     runtime (bounds and nil panics, string counts, locks), which the
//     analysis applies as contract-table entries, not as edges;
//   - dynamic: also every candidate of every call through an interface or
//     func value (the plan's dispatch nodes, without its origin analysis): the
//     largest the graph can be without the rule;
//   - dynamic-iface and dynamic-func: each kind of dynamic call alone.
//
// The arena estimate is the plan's per-SCC working set, estimated from the
// number of SSA values of the SCC's functions at ValueBytes each.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/types2"
)

// ValueBytes is the estimated arena cost of one SSA value of lowered code in
// the solver: an EIR instruction (about 32 bytes), its register's points-to
// set in a hash-consed table and its share of the constraint graph's edges
// and worklists. It is an estimate, stated so that it can be checked against
// the solver once it exists.
const ValueBytes = 160

func main() {
	top := flag.Int("top", 10, "print the functions of the largest SCCs, up to this many SCCs")
	names := flag.Int("names", 40, "names printed per SCC")
	astMode := flag.Bool("ast", false, "build the graph from the typed AST (class hierarchy analysis) instead of lowering the program")
	ratio := flag.Float64("ratio", 1, "with -ast: SSA values per AST node, for the arena estimate (measure it on a program both modes handle)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: escapescc [flags] dir")
		os.Exit(2)
	}
	cfg := &build.Config{Dir: flag.Arg(0), Stdlib: plutonium.Stdlib()}
	prog, err := build.LoadProgram(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *astMode {
		g := newASTGraph(prog)
		g.unit, g.ratio = "AST nodes", *ratio
		for _, mode := range []mode{modeStatic, modeIface, modeFunc, modeDynamic} {
			g.report(mode, *top, *names)
		}
		return
	}
	cf, err := lower.LowerFacts(&lower.Config{Sizes: types2.SizesFor("gc", "amd64")}, prog)
	if cf == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "lowering reported errors; measuring the functions lowered: %v\n", firstLine(err.Error()))
	}
	g := newGraph(cf)
	for _, mode := range []mode{modeStatic, modeContract, modeIface, modeFunc, modeDynamic} {
		g.report(mode, *top, *names)
	}
}

type mode int

const (
	modeStatic mode = iota
	modeContract
	modeIface
	modeFunc
	modeDynamic
)

func (m mode) String() string {
	return [...]string{"static", "static-contract", "dynamic-iface", "dynamic-func", "dynamic"}[m]
}

// A node is a source function: a declaration (all its instances), a literal,
// a range body, or a body-less function.
type node struct {
	name   string
	values int // SSA values over all its lowered instances
	edges  [5][]int
}

type graph struct {
	nodes []*node
	unit  string  // what node sizes count: "SSA values" or "AST nodes"
	ratio float64 // SSA values per unit, for the arena estimate
}

func newGraph(cf *lower.CallFacts) *graph {
	g := &graph{unit: "SSA values", ratio: 1}
	ids := map[any]int{}
	idOf := func(sym *ssa.Sym) int {
		o := cf.Origin(sym)
		var k any = sym
		switch {
		case o.Lit != nil:
			k = o.Lit
		case o.Range != nil:
			k = o.Range
		case o.Obj != nil:
			k = o.Obj
		case o.Decl != nil:
			k = o.Decl
		}
		if id, ok := ids[k]; ok {
			return id
		}
		id := len(g.nodes)
		ids[k] = id
		name := sym.Name
		if o.Obj != nil {
			name = o.Obj.FullName()
		}
		g.nodes = append(g.nodes, &node{name: name})
		return id
	}
	type edgeKey struct{ from, to int }
	seen := [5]map[edgeKey]bool{{}, {}, {}, {}, {}}
	addEdge := func(m mode, from, to int) {
		k := edgeKey{from, to}
		if !seen[m][k] {
			seen[m][k] = true
			g.nodes[from].edges[m] = append(g.nodes[from].edges[m], to)
		}
	}
	for _, f := range cf.Funcs() {
		if !cf.Origin(f.Sym).Source() {
			continue // seen through, from its callers
		}
		from := idOf(f.Sym)
		for _, b := range f.Blocks {
			g.nodes[from].values += len(b.Values)
			for _, v := range b.Values {
				if v.Op == ssa.OpFuncAddr {
					if sym, ok := v.Aux.(*ssa.Sym); ok {
						for _, s := range cf.SourceTargets(sym) {
							to := idOf(s)
							for m := modeStatic; m <= modeDynamic; m++ {
								addEdge(m, from, to)
							}
						}
					}
				}
			}
		}
		for _, c := range cf.Calls(f) {
			var ms []mode
			switch c.Kind {
			case lower.CallStatic:
				ms = []mode{modeStatic, modeIface, modeFunc, modeDynamic}
				if !c.Implicit && !strings.HasPrefix(c.Static.Name, "pluto/rt.panic") {
					ms = append(ms, modeContract)
				}
			case lower.CallIface, lower.CallDropAdapt:
				ms = []mode{modeIface, modeDynamic}
			case lower.CallFuncVal:
				ms = []mode{modeFunc, modeDynamic}
			}
			for _, t := range c.Targets {
				for _, s := range cf.SourceTargets(t) {
					to := idOf(s)
					for _, m := range ms {
						addEdge(m, from, to)
					}
				}
			}
		}
	}
	return g
}

// sccs returns the SCCs of the graph of mode m (Tarjan, iterative).
func (g *graph) sccs(m mode) [][]int {
	n := len(g.nodes)
	index := make([]int, n)
	low := make([]int, n)
	on := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	var stack []int
	var out [][]int
	next := 0
	type frame struct{ v, i int }
	for root := 0; root < n; root++ {
		if index[root] >= 0 {
			continue
		}
		work := []frame{{root, 0}}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		on[root] = true
		for len(work) > 0 {
			fr := &work[len(work)-1]
			es := g.nodes[fr.v].edges[m]
			if fr.i < len(es) {
				w := es[fr.i]
				fr.i++
				if index[w] < 0 {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					on[w] = true
					work = append(work, frame{w, 0})
				} else if on[w] {
					low[fr.v] = min(low[fr.v], index[w])
				}
				continue
			}
			v := fr.v
			work = work[:len(work)-1]
			if len(work) > 0 {
				p := work[len(work)-1].v
				low[p] = min(low[p], low[v])
			}
			if low[v] == index[v] {
				var comp []int
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					on[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				out = append(out, comp)
			}
		}
	}
	return out
}

func (g *graph) report(m mode, top, names int) {
	comps := g.sccs(m)
	sort.Slice(comps, func(i, j int) bool { return len(comps[i]) > len(comps[j]) })
	edges, total := 0, 0
	for _, nd := range g.nodes {
		edges += len(nd.edges[m])
		total += nd.values
	}
	hist := map[string]int{}
	for _, c := range comps {
		switch n := len(c); {
		case n == 1:
			hist["1"]++
		case n <= 10:
			hist["2-10"]++
		case n <= 100:
			hist["11-100"]++
		case n <= 1000:
			hist["101-1000"]++
		default:
			hist[">1000"]++
		}
	}
	fmt.Printf("== %s: %d functions, %d edges, %d %s, %d SCCs (sizes: 1: %d, 2-10: %d, 11-100: %d, 101-1000: %d, >1000: %d)\n",
		m, len(g.nodes), edges, total, g.unit, len(comps), hist["1"], hist["2-10"], hist["11-100"], hist["101-1000"], hist[">1000"])
	for i, c := range comps {
		if i >= top || len(c) < 2 {
			break
		}
		values := 0
		for _, id := range c {
			values += g.nodes[id].values
		}
		fmt.Printf("  SCC %d: %d functions, %d %s, estimated arena %.1f MB\n", i+1, len(c), values, g.unit, float64(values)*g.ratio*ValueBytes/(1<<20))
		ns := make([]string, 0, len(c))
		for _, id := range c {
			ns = append(ns, g.nodes[id].name)
		}
		sort.Strings(ns)
		for j, n := range ns {
			if j >= names {
				fmt.Printf("    ... and %d more\n", len(ns)-names)
				break
			}
			fmt.Printf("    %s\n", n)
		}
	}
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
