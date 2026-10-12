// Package audit reports source-level unsafe operations and their safe callers
// under language sections 18.12 and 21.5. It never executes or links source.
package audit

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/directive"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/race"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

type site struct {
	pos  token.Pos
	text string
}
type edge struct {
	target *function
	pos    token.Pos
	kind   string
}
type function struct {
	pkg    *load.Package
	name   string
	pos    token.Pos
	body   ast.Node
	obj    *types2.Func
	sig    *types2.Signature
	unsafe []site
	calls  []edge
}
type graph struct {
	prog       *load.Program
	functions  []*function
	decls      map[*types2.Func]*function
	literals   map[*ast.FuncLit]*function
	values     []value
	operations []operation
	allocator  *types2.Interface
	moves      *moves.Info
	typeArgs   map[*types2.TypeParam][]types2.Type
}
type value struct {
	fn  *function
	sig *types2.Signature
}
type operation struct {
	sig    *types2.Signature
	reason string
}

// Write writes a deterministic audit of all loaded source, including unused
// declarations. Unsafe labels describe direct operations only. Safe callers
// retain their label and show a finite path to each possible unsafe target.
func Write(w io.Writer, prog *load.Program) error {
	return WriteSelected(w, prog, prog.Packages)
}

// WriteSelected limits the report to selected packages, retaining dependencies
// in the graph so a package pattern does not repeat their reports.
func WriteSelected(w io.Writer, prog *load.Program, selected []*load.Package) error {
	calls, escErr := escape.Analyze(prog)
	mv, moveErr := moves.AnalyzeWith(prog, calls)
	if escErr != nil {
		return escErr
	}
	if moveErr != nil {
		return moveErr
	}
	if err := race.CheckWith(prog, mv); err != nil {
		return err
	}
	g := newGraph(prog)
	g.moves = mv
	for _, f := range g.functions {
		g.scan(f)
	}
	show := map[*load.Package]bool{}
	for _, p := range selected {
		show[p] = true
	}
	var b strings.Builder
	last := ""
	for _, f := range g.functions {
		if !show[f.pkg] {
			continue
		}
		paths := g.paths(f)
		if len(f.unsafe) == 0 && len(paths) == 0 {
			continue
		}
		if last != f.pkg.Path {
			fmt.Fprintf(&b, "package %s\n", f.pkg.Path)
			last = f.pkg.Path
		}
		label := "safe"
		if len(f.unsafe) > 0 {
			label = "unsafe"
		}
		fmt.Fprintf(&b, "  %s %s %s\n", label, f.name, g.position(f.pos))
		for _, s := range f.unsafe {
			fmt.Fprintf(&b, "    %s: %s\n", g.position(s.pos), s.text)
		}
		for _, p := range paths {
			fmt.Fprintf(&b, "    calls %s\n", p[len(p)-1].target.name)
			for _, e := range p {
				fmt.Fprintf(&b, "      %s: %s %s\n", g.position(e.pos), e.kind, e.target.name)
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func (g *graph) position(pos token.Pos) string {
	p := g.prog.Fset.Position(pos)
	return fmt.Sprintf("%s:%d:%d", g.prog.TrimmedName(p.Filename), p.Line, p.Column)
}

func newGraph(prog *load.Program) *graph {
	g := &graph{prog: prog, decls: map[*types2.Func]*function{}, literals: map[*ast.FuncLit]*function{}, typeArgs: map[*types2.TypeParam][]types2.Type{}}
	for _, pkg := range prog.Packages {
		if pkg.Path == "pluto/rt" {
			if o := pkg.Types.Scope().Lookup("Allocator"); o != nil {
				g.allocator, _ = o.Type().Underlying().(*types2.Interface)
			}
		}
		for _, file := range pkg.Files {
			for _, d := range file.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					o, _ := pkg.Info.Defs[d.Name].(*types2.Func)
					f := &function{pkg: pkg, name: functionName(o), pos: d.Pos(), body: d, obj: o, sig: o.Type().(*types2.Signature)}
					g.decls[o] = f
					g.functions = append(g.functions, f)
					if d.Body == nil {
						f.unsafe = append(f.unsafe, site{d.Pos(), "function without a body"})
					}
					if d.Doc != nil {
						for _, c := range d.Doc.List {
							n, _, ok := directive.Split(c.Text)
							if ok && (n == "noescape" || n == "resultholds" || n == "nochecks" || n == "extern") {
								f.unsafe = append(f.unsafe, site{c.Pos(), "//pu:" + n})
							}
						}
					}
				case *ast.GenDecl:
					if d.Tok != token.VAR {
						continue
					}
					for _, s := range d.Specs {
						s := s.(*ast.ValueSpec)
						if len(s.Values) == 0 {
							continue
						}
						f := &function{pkg: pkg, name: pkg.Path + ".initializer@" + g.position(s.Pos()), pos: s.Pos(), body: s}
						g.functions = append(g.functions, f)
					}
				}
			}
		}
	}
	// Discover literals before resolving calls, including nested literals.
	for _, f := range slices.Clone(g.functions) {
		ast.Inspect(f.body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				lf := &function{pkg: f.pkg, name: f.name + ".func@" + g.position(lit.Pos()), pos: lit.Pos(), body: lit.Body, sig: f.pkg.Info.TypeOf(lit).(*types2.Signature)}
				g.functions = append(g.functions, lf)
				g.literals[lit] = lf
				g.values = append(g.values, value{lf, lf.sig})
			}
			return true
		})
	}
	slices.SortFunc(g.functions, func(a, b *function) int {
		if c := strings.Compare(a.pkg.Path, b.pkg.Path); c != 0 {
			return c
		}
		pa, pb := prog.Fset.Position(a.pos), prog.Fset.Position(b.pos)
		if c := strings.Compare(pa.Filename, pb.Filename); c != 0 {
			return c
		}
		return pa.Offset - pb.Offset
	})
	g.collectValues()
	return g
}

func functionName(o *types2.Func) string {
	n := o.Pkg().Path() + "."
	if r := o.Type().(*types2.Signature).Recv(); r != nil {
		n += types2.TypeString(r.Type(), func(*types2.Package) string { return "" }) + "."
	}
	return n + o.Name()
}

// paths finds one shortest path to each unsafe function. A visited set bounds
// recursive call cycles; sorted source edges make ties reproducible.
func (g *graph) paths(start *function) [][]edge {
	if len(start.unsafe) > 0 {
		return nil
	}
	type entry struct {
		fn   *function
		path []edge
	}
	q := []entry{{fn: start}}
	seen := map[*function]bool{start: true}
	var out [][]edge
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		for _, e := range n.fn.calls {
			if seen[e.target] {
				continue
			}
			seen[e.target] = true
			p := append(slices.Clone(n.path), e)
			if len(e.target.unsafe) > 0 {
				out = append(out, p)
			} else {
				q = append(q, entry{e.target, p})
			}
		}
	}
	return out
}
