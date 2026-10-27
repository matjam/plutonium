package load

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/printer"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// The type checker shows the fix for an owner type declaration as printed
// source, but does not import the printer (types2.DeclPrinter). This file
// links it to the printer.
func init() {
	types2.DeclPrinter = printDecl
}

func printDecl(fset *token.FileSet, decl *ast.GenDecl) (string, error) {
	buf := pu.Alloc[compat.Buffer]()
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(buf, fset, decl); err != nil {
		return "", err
	}
	return buf.String(), nil
}
