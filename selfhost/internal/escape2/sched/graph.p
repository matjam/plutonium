// Translated by go2pu from internal/escape2/sched/graph.go at the go-cutover tag; edited by hand since.

package sched

import (
	"selfhost/compat"
	"selfhost/internal/escape2/eir"
	"selfhost/pu"
)

// The call graph for scheduling (plan section (b)). Nodes are the program's
// functions, numbered by FuncID, then one dispatch node per distinct
// candidate set: a dynamic call in f is an edge f→D, and D has an edge to
// every function the dispatch rule says the call may reach. Dispatch nodes
// keep the graph's size linear in the call sites when many calls share a
// set; an SCC made only of dispatch nodes has nothing to solve.
//
// Contract-table calls have no edge: their summaries come from the table.

type callGraph struct {
	nFuncs int
	succ   [][]int32 // by node
	// deps[f] are the functions whose summaries f reads: its static callees
	// and the callees of its dynamic calls, each once.
	deps [][]eir.FuncID
}

func buildGraph(p *eir.Program, rule dispatchRule) *callGraph {
	return buildGraphD(p, rule, nil)
}

// buildGraphD is buildGraph with the deferred calls of D49 (deferral.go):
// the callers of a function with a deferred call also depend on what they
// pass where it is deferred. A deferred call keeps its edges to its
// candidates: where its arguments turn out to hold a pointer into the
// frame when the solver runs, the function itself resolves it against
// them (the join, as for a call that is not deferred), so they must be
// solved before it or with it. Only the summaries a solve reads re-solve
// it (run.go), so the edges cost nothing where they are not read.
func buildGraphD(p *eir.Program, rule dispatchRule, def *deferral) *callGraph {
	n := len(p.Funcs)
	g := pu.New(callGraph{nFuncs: n, succ: compat.Make[[]int32](n), deps: compat.Make[[]eir.FuncID](n)})
	dispatch := map[string]int32{}
	var key []byte
	for _, f := range p.Funcs {
		seen := map[int32]bool{}
		dseen := map[eir.FuncID]bool{}
		addDep := func(id eir.FuncID) {
			if !dseen[id] {
				dseen[id] = true
				g.deps[f.ID] = compat.AppendPlain(g.deps[f.ID], id)
			}
		}
		edge := func(to int32) {
			if !seen[to] {
				seen[to] = true
				g.succ[f.ID] = compat.AppendPlain(g.succ[f.ID], to)
			}
		}
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op != eir.OpCall || in.Call == nil {
					continue
				}
				c := in.Call
				switch c.Kind {
				case eir.CallStatic:
					if c.Callee != eir.NoFunc && int(c.Callee) < n {
						edge(int32(c.Callee))
						addDep(c.Callee)
					}
				case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
					ids := rule.callees(f, c)
					if len(ids) == 0 {
						continue
					}
					for _, id := range ids {
						addDep(id)
					}
					if len(ids) == 1 {
						edge(int32(ids[0]))
						continue
					}
					// The key is text: the self-hosted compiler's strings
					// hold valid UTF-8, and its conversion of other bytes
					// would make distinct keys equal.
					key = key[:0]
					for _, id := range ids {
						key = compat.AppendInt(key, int64(id), 10)
						key = compat.AppendPlain(key, ' ')
					}
					d, ok := dispatch[compat.String(key)]
					if !ok {
						d = int32(len(g.succ))
						dispatch[compat.String(key)] = d
						out := compat.Make[int32](len(ids))
						for j, id := range ids {
							out[j] = int32(id)
						}
						g.succ = compat.Append(g.succ, out)
					}
					edge(d)
				}
			}
		}
		if def != nil {
			// What f's solve reads to resolve its callees' deferred calls.
			for _, id := range def.deps(f) {
				edge(int32(id))
				addDep(id)
			}
		}
	}
	return g
}

// sccs returns the strongly connected components of the graph's function
// nodes, callees first (Tarjan's algorithm, iterative so that deep call
// chains do not exhaust the stack). Each component lists its functions in
// increasing order; components of dispatch nodes alone are left out.
func (g *callGraph) sccs() [][]eir.FuncID {
	n := len(g.succ)
	index := compat.Make[int32](n)
	low := compat.Make[int32](n)
	onStack := compat.Make[bool](n)
	for i := range index {
		index[i] = -1
	}
	var stack []int32
	var out [][]eir.FuncID
	next := int32(0)

	type frame struct {
		v    int32
		edge int
	}
	var call []frame
	for root := 0; root < n; root++ {
		if index[root] >= 0 {
			continue
		}
		call = compat.Append(call[:0], frame{v: int32(root)})
		index[root], low[root] = next, next
		next++
		stack = compat.AppendPlain(stack, int32(root))
		onStack[root] = true
		for len(call) > 0 {
			fr := &call[len(call)-1]
			v := fr.v
			if fr.edge < len(g.succ[v]) {
				w := g.succ[v][fr.edge]
				fr.edge++
				if index[w] < 0 {
					index[w], low[w] = next, next
					next++
					stack = compat.AppendPlain(stack, w)
					onStack[w] = true
					call = compat.Append(call, frame{v: w})
				} else if onStack[w] && index[w] < low[v] {
					low[v] = index[w]
				}
				continue
			}
			// v is done.
			call = call[:len(call)-1]
			if len(call) > 0 {
				u := call[len(call)-1].v
				if low[v] < low[u] {
					low[u] = low[v]
				}
			}
			if low[v] != index[v] {
				continue
			}
			var comp []eir.FuncID
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				if int(w) < g.nFuncs {
					comp = compat.AppendPlain(comp, eir.FuncID(w))
				}
				if w == v {
					break
				}
			}
			if len(comp) > 0 {
				sortIDs(comp)
				out = compat.Append(out, comp)
			}
		}
	}
	return out
}
