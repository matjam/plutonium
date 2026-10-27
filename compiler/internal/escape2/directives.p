package escape2

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/syntax/ast"
	"compiler/pu"
	"pluto/strings"
)

// directiveErrors reports the //pu:resultholds directives the analysis
// cannot take (section 20.3): one outside the standard library (E3008),
// and a name in one that is not a parameter of the function (E3007).
func directiveErrors(prog *load.Program) []*diag.Diagnostic {
	var out []*diag.Diagnostic
	for _, p := range prog.Packages {
		for _, file := range p.Files {
			for _, d := range file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Doc == nil {
					continue
				}
				for _, c := range fd.Doc.List {
					rest, ok := strings.CutPrefix(c.Text, "//pu:resultholds")
					if !ok || rest != "" && rest[0] != ' ' && rest[0] != '\t' {
						continue
					}
					pos := prog.Fset.Position(fd.Pos())
					if !strings.HasPrefix(pos.Filename, load.StdPrefix) {
						out = compat.Append(out, diag.New(pos, diag.ResultHoldsOutsideStd,
							"//pu:resultholds is for the standard library; %s cannot use it", pu.Box(fd.Name.Name)))
						continue
					}
					for _, name := range compat.Fields(rest) {
						if !isParam(fd, name) {
							out = compat.Append(out, diag.New(pos, diag.BadResultHolds,
								"//pu:resultholds names %s, which is not a parameter of %s", pu.Box(name), pu.Box(fd.Name.Name)))
						}
					}
				}
			}
		}
	}
	return out
}

// isParam reports whether name is the receiver or a parameter of fd.
func isParam(fd *ast.FuncDecl, name string) bool {
	lists := []*ast.FieldList{fd.Recv, fd.Type.Params}
	for _, l := range lists {
		if l == nil {
			continue
		}
		for _, f := range l.List {
			for _, n := range f.Names {
				if n.Name == name {
					return true
				}
			}
		}
	}
	return false
}
