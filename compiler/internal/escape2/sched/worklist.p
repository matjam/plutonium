// Translated by go2pu from internal/escape2/sched/worklist.go at the go-cutover tag; edited by hand since.

package sched

import (
	"pluto/container/heap"
	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/pu"
)

// sccWork is the worklist of one SCC's iteration. Functions are solved in a
// postorder of the SCC's own call graph, callees first: a function waiting
// in the list is solved after every queued function that comes before it in
// that order, so a caller is not solved again and again while its callees
// are still growing. Ties do not arise: each function has its own rank.
// On the translated compiler (compiler/cmd/pluto, 2,235 functions in one
// SCC) this order took 4 min 13 s against 4 min 39 s first in, first out,
// with less memory; it solves more often, but cheaper solves.
// Every function is in the list once at most.
// sccOrderReversed, set by ESCAPE2_SCC_ORDER=reverse, solves callers first:
// for checking that the result does not depend on the order (a debugging
// knob, never for the compiler).
var sccOrderReversed = compat.Getenv("ESCAPE2_SCC_ORDER") == "reverse"

type sccWork struct {
	rank   map[eir.FuncID]int
	byRank []eir.FuncID
	queued []bool // by rank
	h      intHeap
}

// newSCCWork makes the worklist of comp, every function queued. deps gives
// the functions a function reads (its callees and candidates).
func newSCCWork(comp []eir.FuncID, deps func(eir.FuncID) []eir.FuncID) *sccWork {
	in := make(map[eir.FuncID]bool, len(comp))
	for _, id := range comp {
		in[id] = true
	}
	w := pu.New(sccWork{rank: make(map[eir.FuncID]int, len(comp))})
	// Iterative depth-first postorder over the SCC's own edges, from each
	// function in turn (comp's order, so the result is deterministic).
	visited := make(map[eir.FuncID]bool, len(comp))
	type frame struct {
		id   eir.FuncID
		next int
	}
	var stack []frame
	for _, root := range comp {
		if visited[root] {
			continue
		}
		visited[root] = true
		stack = compat.Append(stack[:0], frame{id: root})
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			ds := deps(top.id)
			if top.next < len(ds) {
				d := ds[top.next]
				top.next++
				if in[d] && !visited[d] {
					visited[d] = true
					stack = compat.Append(stack, frame{id: d})
				}
				continue
			}
			w.rank[top.id] = len(w.byRank)
			w.byRank = compat.AppendPlain(w.byRank, top.id)
			stack = stack[:len(stack)-1]
		}
	}
	if sccOrderReversed {
		for i, j := 0, len(w.byRank)-1; i < j; i, j = i+1, j-1 {
			w.byRank[i], w.byRank[j] = w.byRank[j], w.byRank[i]
		}
		for r, id := range w.byRank {
			w.rank[id] = r
		}
	}
	w.queued = compat.Make[bool](len(w.byRank))
	for r := range w.byRank {
		w.queued[r] = true
		w.h = intHeap(compat.AppendPlain(w.h, r))
	}
	heap.Init(&w.h)
	return w
}

func (w *sccWork) empty() bool { return len(w.h) == 0 }

// pop returns the queued function first in the order.
func (w *sccWork) pop() eir.FuncID {
	r := heap.Pop(&w.h).(int)
	w.queued[r] = false
	return w.byRank[r]
}

// push queues id, a function of the SCC, unless it is queued.
func (w *sccWork) push(id eir.FuncID) {
	r, ok := w.rank[id]
	if !ok || w.queued[r] {
		return
	}
	w.queued[r] = true
	heap.Push(&w.h, r)
}

type intHeap []int

func (h intHeap) Len() int           { return len(h) }
func (h intHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *intHeap) Push(x any)        { *h = intHeap(compat.AppendPlain(*h, x.(int))) }
func (h *intHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
