package load

import (
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/printer"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// The type checker shows the fix for an owner type declaration as printed
// source, but does not import the printer (types2.DeclPrinter). This file is
// the Go build's link to it; the translation to Plutonium has its own
// printhook.p until the printer is translated (self-hosting plan, section 8).
func init() {
	types2.DeclPrinter = func(fset *token.FileSet, decl *ast.GenDecl) (string, error) {
		var buf strings.Builder
		cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
		if err := cfg.Fprint(&buf, fset, decl); err != nil {
			return "", err
		}
		return buf.String(), nil
	}
}
