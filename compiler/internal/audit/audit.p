// Translated by go2pu from internal/audit/audit.go; edited by hand since.

// Package audit reports source-level unsafe operations and their safe callers
// under language sections 18.12 and 21.5. It never executes or links source.
package audit

import (
	"compiler/compat"
	"compiler/internal/directive"
	"compiler/internal/escape2"
	"compiler/internal/load"
	"compiler/internal/moves"
	"compiler/internal/race"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/io"
	"pluto/slices"
	"pluto/strings"
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
	calls, escErr := escape2.CheckCalls(prog)
	mv, moveErr := moves.AnalyzeWith(prog, calls)
	if escErr != nil {
		return escErr
	}
	if moveErr != nil {
		return moveErr
	}
	race.CheckWith(prog, mv) or return
	g := newGraph(prog)
	g.moves = mv
	for _, f := range g.functions {
		g.scan(f)
	}
	show := map[*load.Package]bool{}
	for _, p := range selected {
		show[p] = true
	}
	b := pu.Alloc[compat.Builder]()
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
			compat.Fprintfb(b, "package %s\n", compat.ArgS(f.pkg.Path))
			last = f.pkg.Path
		}
		label := "safe"
		if len(f.unsafe) > 0 {
			label = "unsafe"
		}
		compat.Fprintfb(b, "  %s %s %s\n", compat.ArgS(label), compat.ArgS(f.name), compat.ArgS(g.position(f.pos)))
		for _, s := range f.unsafe {
			compat.Fprintfb(b, "    %s: %s\n", compat.ArgS(g.position(s.pos)), compat.ArgS(s.text))
		}
		for _, p := range paths {
			compat.Fprintfb(b, "    calls %s\n", compat.ArgS(p[len(p)-1].target.name))
			for _, e := range p {
				compat.Fprintfb(b, "      %s: %s %s\n", compat.ArgS(g.position(e.pos)), compat.ArgS(e.kind), compat.ArgS(e.target.name))
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// regionCall reports whether a bodyless declaration is one of the calls that
// open and close regions: they cannot make an access invalid, and the race
// analysis follows them, so they are not unsafe code (language spec section
// 18.12).
func regionCall(pkg, name string) bool {
	if pkg != "pluto/arch/x86" {
		return false
	}
	switch name {
	case "Cli", "Sti", "SaveFlagsCli", "RestoreFlags":
		return true
	}
	return false
}

func (g *graph) position(pos token.Pos) string {
	p := g.prog.Fset.Position(pos)
	return compat.Sprintfb("%s:%d:%d", compat.ArgS(g.prog.TrimmedName(p.Filename)), compat.ArgI(int64(p.Line)), compat.ArgI(int64(p.Column)))
}

func newGraph(prog *load.Program) *graph {
	g := pu.New(graph{prog: prog, decls: map[*types2.Func]*function{}, literals: map[*ast.FuncLit]*function{}, typeArgs: map[*types2.TypeParam][]types2.Type{}})
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
					f := pu.New(function{pkg: pkg, name: functionName(o), pos: d.Pos(), body: d, obj: o, sig: o.Type().(*types2.Signature)})
					g.decls[o] = f
					g.functions = compat.Append(g.functions, f)
					if d.Body == nil && !regionCall(pkg.Path, d.Name.Name) {
						f.unsafe = compat.Append(f.unsafe, site{d.Pos(), "function without a body"})
					}
					if d.Doc != nil {
						for _, c := range d.Doc.List {
							n, _, ok := directive.Split(c.Text)
							if ok && (n == "noescape" || n == "resultholds" || n == "nochecks" || n == "extern") {
								f.unsafe = compat.Append(f.unsafe, site{c.Pos(), compat.Concat("//pu:", n)})
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
						f := pu.New(function{pkg: pkg, name: compat.Concat(pkg.Path, ".initializer@", g.position(s.Pos())), pos: s.Pos(), body: s})
						g.functions = compat.Append(g.functions, f)
					}
				}
			}
		}
	}
	// Discover literals before resolving calls, including nested literals.
	for _, f := range compat.Clone(g.functions) {
		ast.Inspect(f.body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				lf := pu.New(function{pkg: f.pkg, name: compat.Concat(f.name, ".func@", g.position(lit.Pos())), pos: lit.Pos(), body: lit.Body, sig: f.pkg.Info.TypeOf(lit).(*types2.Signature)})
				g.functions = compat.Append(g.functions, lf)
				g.literals[lit] = lf
				g.values = compat.Append(g.values, value{lf, lf.sig})
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
	n := compat.Concat(o.Pkg().Path(), ".")
	if r := o.Type().(*types2.Signature).Recv(); r != nil {
		n = compat.Concat(n, compat.Concat(types2.TypeString(r.Type(), func(*types2.Package) string { return "" }), "."))
	}
	return compat.Concat(n, o.Name())
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
			p := compat.Append(compat.Clone(n.path), e)
			if len(e.target.unsafe) > 0 {
				out = compat.Append(out, p)
			} else {
				q = compat.Append(q, entry{e.target, p})
			}
		}
	}
	return out
}
