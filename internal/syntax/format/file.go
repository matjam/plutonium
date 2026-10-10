package format

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// File formats the source file filename, as gofmt formats a file: the
// name appears in syntax errors, and with fragmentOk a list of declarations
// or statements is accepted in place of a complete file, as gofmt accepts
// one on standard input.
func File(filename string, src []byte, fragmentOk bool) ([]byte, error) {
	fset := token.NewFileSet()
	file, sourceAdj, indentAdj, err := parse(fset, filename, src, fragmentOk)
	if err != nil {
		return nil, err
	}
	if sourceAdj == nil {
		ast.SortImports(fset, file)
	}
	return format(fset, file, sourceAdj, indentAdj, src, config)
}
