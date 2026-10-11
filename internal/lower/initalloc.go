package lower

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Allocation during package initialization (language spec section 15.2).
// A freestanding program, and so every kernel, has no allocator while its
// packages are initialized, and no console to report a failure on. So in a
// freestanding build a package-level initializer or an init function that
// can reach an allocation is a compile error: new, make, a map literal, an
// operation that makes a string at run time, or any call that reaches one.
// checkInitAlloc walks the call graph from each package's initialization,
// as checkContexts does from interrupt handlers, and stops at the runtime
// functions the compiler calls to allocate (allocCalls). It does not follow
// what an earlier init function installs: doubt resolves to an error.

// An initRoot is one piece of a package's initialization: the function that
// initializes its package-level variables, or one of its init functions.
type initRoot struct {
	sym  *ssa.Sym
	pkg  *load.Package
	decl *ast.FuncDecl // the init function, or nil for the variables
}

// checkInitAlloc reports, in a freestanding build, each part of package
// initialization that can reach an allocation.
func (l *Lowerer) checkInitAlloc(g *callGraph) {
	if !l.cfg.Freestanding && !l.cfg.Kernel {
		return
	}
	inits := map[*ssa.Sym]bool{}
	for _, r := range l.initRoots {
		inits[r.sym] = true
	}
	// Each package's variables first, then its init functions, as they run.
	// (queueInit adds a package's parts together, its init functions first.)
	var roots []initRoot
	for i := 0; i < len(l.initRoots); {
		j := i
		for j < len(l.initRoots) && l.initRoots[j].pkg == l.initRoots[i].pkg {
			j++
		}
		part := l.initRoots[i:j]
		for _, r := range part {
			if r.decl == nil {
				roots = append(roots, r)
			}
		}
		for _, r := range part {
			if r.decl != nil {
				roots = append(roots, r)
			}
		}
		i = j
	}
	for _, r := range roots {
		if g.funcs[r.sym] == nil {
			continue
		}
		root := &ctxNode{sym: r.sym}
		if r.decl != nil {
			if path, last := g.searchAlloc(root, inits); last != nil {
				l.reportInitAlloc(g, r, path, last)
			}
			continue
		}
		// The variables of a package are initialized by one function:
		// search from each call it makes, and report each variable once.
		done := map[*types2.Var]bool{}
		hops := append(g.allocatorCalls(r.sym), g.callsOf(r.sym)...)
		slices.SortStableFunc(hops, func(a, b hop) int { return cmp.Compare(a.pos, b.pos) })
		for i := range hops {
			h := &hops[i]
			v := l.initVarAt(r.pkg, h.pos)
			if v == nil || done[v] || inits[h.to] {
				continue
			}
			if h.alloc || allocCalls[h.to.Name] {
				done[v] = true
				l.reportInitAlloc(g, r, root, h)
				continue
			}
			if path, last := g.searchAlloc(&ctxNode{sym: h.to, via: h, parent: root}, inits); last != nil {
				done[v] = true
				l.reportInitAlloc(g, r, path, last)
			}
		}
	}
}

// searchAlloc walks the calls from start breadth first, not entering the
// functions in skip (other parts of initialization, checked on their own),
// and returns the first call of an allocating runtime function, with the
// function that makes it.
func (g *callGraph) searchAlloc(start *ctxNode, skip map[*ssa.Sym]bool) (*ctxNode, *hop) {
	seen := map[*ssa.Sym]bool{start.sym: true}
	queue := []*ctxNode{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if hs := g.allocatorCalls(n.sym); len(hs) > 0 {
			return n, &hs[0]
		}
		for i := range g.callsOf(n.sym) {
			h := &g.edges[n.sym][i]
			if allocCalls[h.to.Name] {
				return n, h
			}
			if skip[h.to] || seen[h.to] {
				continue
			}
			seen[h.to] = true
			queue = append(queue, &ctxNode{sym: h.to, via: h, parent: n})
		}
	}
	return nil, nil
}

// allocatorCalls returns the calls that sym makes of the Alloc method of
// an rt.Allocator, through the interface. Library code allocates that way
// from the allocator it is given or the default one.
func (g *callGraph) allocatorCalls(sym *ssa.Sym) []hop {
	f := g.funcs[sym]
	if f == nil {
		return nil
	}
	var out []hop
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCallInd && v.Call != nil && v.Call.Alloc == ssa.AllocAlloc {
				out = append(out, hop{from: sym, to: sym, pos: v.Pos, site: g.l.sites[v], alloc: true})
			}
		}
	}
	return out
}

// reportInitAlloc reports that the initialization part r reaches the
// allocation last, through the calls that lead to n.
func (l *Lowerer) reportInitAlloc(g *callGraph, r initRoot, n *ctxNode, last *hop) {
	var hops []hop
	for ; n.via != nil; n = n.parent {
		hops = append(hops, *n.via)
	}
	for i, j := 0, len(hops)-1; i < j; i, j = i+1, j-1 {
		hops[i], hops[j] = hops[j], hops[i]
	}
	hops = append(hops, *last)
	var vis []hop
	for _, h := range hops {
		if len(vis) > 0 && g.synthesized(h.from) {
			vis[len(vis)-1].to = h.to
			continue
		}
		vis = append(vis, h)
	}
	// How the library allocates is not where the program does: end at the
	// call that enters it.
	if k := libraryEntry(vis); k >= 0 {
		vis = vis[:k+1]
	}
	var phrases []string
	for i := range vis {
		h := &vis[i]
		s := l.hopPhrase(h, false, opWait)
		switch {
		case h.alloc:
			s = "allocates"
			if h.site != nil && h.site.desc != "" {
				s += " with " + h.site.desc
			}
			s += " (" + l.shortPos(h.pos) + ")"
		case h.site == nil && strings.HasPrefix(h.to.Name, "pluto/rt.mapMake"):
			s = "makes a map (" + l.shortPos(h.pos) + ")"
		case i == len(vis)-1 && !strings.HasPrefix(s, "allocates"):
			s += ", which allocates"
		}
		phrases = append(phrases, s)
	}
	pos := vis[0].pos

	var who, subject, fix string
	switch {
	case r.decl != nil:
		who = fmt.Sprintf("the init function at %s", l.shortPos(r.decl.Pos()))
		subject = "The init function"
		if r.pkg != l.prog.Main {
			who = fmt.Sprintf("the init function of package %s (%s)", r.pkg.Types.Name(), l.shortPos(r.decl.Pos()))
		}
		fix = "Do the work of the init function in the program's entry function instead, after it has called alloc.SetDefault."
	default:
		v := l.initVarAt(r.pkg, pos)
		name := "a package-level variable"
		if v != nil {
			name = v.Name()
		}
		who = "the initializer of " + name
		subject = "The initializer of " + name
		fix = fmt.Sprintf("Declare %s without an initializer, and set it in the program's entry function after it has called alloc.SetDefault", name)
		if v != nil {
			fix += fmt.Sprintf(":\n\nvar %s %s\n\nfunc kernelMain(...) {\n\talloc.SetDefault(heap)\n\t%s = ...\n}", v.Name(), types2.TypeString(v.Type(), types2.RelativeTo(r.pkg.Types)), v.Name())
		}
	}
	path := subject + " " + strings.Join(phrases, ", which ") + "."
	text := "A freestanding program, which every kernel is, has no allocator while its packages are initialized: it installs one in its entry function with alloc.SetDefault. Package initialization runs before that, so an allocation there would have nowhere to come from, and no console to report the failure on.\n" + path
	d := &diag.Diagnostic{Code: diag.InitAllocates, Msg: fmt.Sprintf("%s allocates, but a freestanding program has no allocator during package initialization", who)}
	d.Text = text
	intro, code, _ := strings.Cut(fix, ":\n\n")
	d.Fix = &diag.Fix{Intro: intro, Code: code}
	l.report(pos, d)
}

// initVarAt returns the package-level variable of p whose initializer
// contains pos, or nil.
func (l *Lowerer) initVarAt(p *load.Package, pos token.Pos) *types2.Var {
	for _, init := range p.Info.InitOrder {
		if init.Rhs.Pos() <= pos && pos < init.Rhs.End() {
			for _, v := range init.Lhs {
				if v.Name() != "_" {
					return v
				}
			}
			return init.Lhs[0]
		}
	}
	return nil
}
