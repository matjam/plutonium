package lower

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// checkInitTasks enforces section 15.2 before any optimization can remove
// calls. Unlike interrupt checks, waits do not end this walk: a timer's
// start, for example, follows its lock acquisition.
func (l *Lowerer) checkInitTasks(g *callGraph) {
	skip := map[*ssa.Sym]bool{}
	free := map[*ssa.Sym]bool{} // reach no spawn (outside skip)
	for _, r := range l.initRoots {
		skip[r.sym] = true
	}
	for _, r := range l.initRoots {
		root := &ctxNode{sym: r.sym}
		if r.decl != nil {
			if n, last, op := g.searchSpawn(root, skip, free); n != nil {
				l.reportInitTask(g, r, n, last, op)
			}
			continue
		}
		// Each expression has its own diagnostic even though lowering puts
		// all variable initializers in one function.
		done := map[*types2.Var]bool{}
		for _, h := range g.callsOf(r.sym) {
			v := l.initVarAt(r.pkg, h.pos)
			if done[v] || skip[h.to] {
				continue
			}
			if isSpawn(h.to) {
				l.reportInitTask(g, r, root, &h, nil)
				done[v] = true
				continue
			}
			if n, last, op := g.searchSpawn(&ctxNode{sym: h.to, via: &h, parent: root}, skip, free); n != nil {
				l.reportInitTask(g, r, n, last, op)
				done[v] = true
			}
		}
	}
}

// searchSpawn finds the nearest spawn reachable from start, not passing
// through skip. A search that finds none proves every function it visited
// spawn-free, so later searches prune those through free; that leaves
// their results unchanged.
func (g *callGraph) searchSpawn(start *ctxNode, skip, free map[*ssa.Sym]bool) (*ctxNode, *hop, *blockOp) {
	if free[start.sym] {
		return nil, nil, nil
	}
	seen := map[*ssa.Sym]bool{start.sym: true}
	queue := []*ctxNode{start}
	var visited []*ssa.Sym
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		visited = append(visited, n.sym)
		if fb, ok := g.l.bodies[n.sym]; ok {
			for _, op := range blockingOps(fb) {
				if op.kind == opSpawn {
					return n, nil, &op
				}
			}
		}
		for i := range g.callsOf(n.sym) {
			h := &g.edges[n.sym][i]
			if isSpawn(h.to) {
				return n, h, nil
			}
			if skip[h.to] || seen[h.to] || free[h.to] {
				continue
			}
			seen[h.to] = true
			queue = append(queue, &ctxNode{sym: h.to, via: h, parent: n})
		}
	}
	for _, s := range visited {
		free[s] = true
	}
	return nil, nil, nil
}

func (l *Lowerer) reportInitTask(g *callGraph, r initRoot, n *ctxNode, last *hop, op *blockOp) {
	var path []hop
	for ; n.via != nil; n = n.parent {
		path = append(path, *n.via)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	if last != nil {
		path = append(path, *last)
	}
	var visible []hop
	for _, h := range path {
		if len(visible) > 0 && g.synthesized(h.from) {
			visible[len(visible)-1].to = h.to
			continue
		}
		visible = append(visible, h)
	}
	if k := libraryEntry(visible); k >= 0 {
		visible = visible[:k+1]
		op = nil
	}
	pos := token.NoPos
	if op != nil {
		pos = op.pos
	}
	if len(visible) > 0 && visible[0].pos.IsValid() {
		pos = visible[0].pos
	}
	if !pos.IsValid() && r.decl != nil {
		pos = r.decl.Pos()
	}
	who := "the initializer of a package-level variable"
	if r.decl != nil {
		who = fmt.Sprintf("the init function of package %s at %s", r.pkg.Types.Name(), l.shortPos(r.decl.Pos()))
	} else if v := l.initVarAt(r.pkg, pos); v != nil {
		who = "the initializer of " + v.Name()
	}
	var phrases []string
	for i := range visible {
		phrases = append(phrases, l.hopPhrase(&visible[i], op == nil && i == len(visible)-1, opSpawn))
	}
	if op != nil {
		phrases = append(phrases, fmt.Sprintf("starts a task (%s)", l.shortPos(op.pos)))
	}
	d := &diag.Diagnostic{Code: diag.InitSpawns, Msg: who + " can start a task during package initialization"}
	subject := "The" + strings.TrimPrefix(who, "the")
	d.Text = "Package initialization must finish before another task can read package variables.\n" + subject + " " + strings.Join(phrases, ", which ") + "."
	d.Fix = &diag.Fix{Intro: "Start the task from main or a function main calls"}
	l.report(pos, d)
}
