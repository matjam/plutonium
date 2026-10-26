// Translated by go2pu from internal/lsp/symbols.go; edited by hand since.

package lsp

import (
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
)

func (s *server) documentSymbols(p *DocumentSymbolParams) ([]DocumentSymbol, *rpcError) {
	f, _, rerr := s.fileAt(p.TextDocument.URI, Position{})
	if f == nil {
		return nil, rerr
	}
	return fileSymbols(f), nil
}

// fileSymbols returns the outline of a file: its declarations, with the
// fields and methods of types and the members of enums.
func fileSymbols(f *srcFile) []DocumentSymbol {
	syms := compat.Clone([]DocumentSymbol{})
	for _, d := range f.ast.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			kind, name := SymbolFunction, d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = SymbolMethod
				name = compat.Concat("(", recvString(d.Recv.List[0].Type), ").", name)
			}
			detail := ""
			if obj, ok := f.pkg.info.Defs[d.Name].(*types2.Func); ok {
				detail = types2.TypeString(obj.Type(), qualifier(f.pkg.types))
			}
			syms = compat.Append(syms, symbol(f, name, detail, kind, d, d.Name))
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				var node ast.Node = spec
				if len(d.Specs) == 1 {
					node = d
				}
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					syms = compat.Append(syms, typeSymbol(f, spec, node))
				case *ast.ValueSpec:
					kind := SymbolVariable
					if d.Tok == token.CONST {
						kind = SymbolConstant
					}
					for _, name := range spec.Names {
						if name.Name == "_" {
							continue
						}
						detail := ""
						if obj := f.pkg.info.Defs[name]; obj != nil {
							detail = types2.TypeString(obj.Type(), qualifier(f.pkg.types))
						}
						syms = compat.Append(syms, symbol(f, name.Name, detail, kind, node, name))
					}
				}
			}
		}
	}
	return syms
}

func typeSymbol(f *srcFile, spec *ast.TypeSpec, node ast.Node) DocumentSymbol {
	kind := SymbolClass
	var children []DocumentSymbol
	switch t := spec.Type.(type) {
	case *ast.StructType:
		kind = SymbolStruct
		children = fieldSymbols(f, t.Fields, SymbolField)
	case *ast.PackedType:
		kind = SymbolStruct
		children = fieldSymbols(f, t.Fields, SymbolField)
	case *ast.InterfaceType:
		kind = SymbolInterface
		children = fieldSymbols(f, t.Methods, SymbolMethod)
	case *ast.EnumType:
		kind = SymbolEnum
		for _, m := range t.Members {
			children = compat.Append(children, symbol(f, m.Name.Name, "", SymbolEnumMember, m, m.Name))
		}
	}
	detail := ""
	if obj := f.pkg.info.Defs[spec.Name]; obj != nil {
		if kind == SymbolClass {
			detail = types2.TypeString(obj.Type().Underlying(), qualifier(f.pkg.types))
		}
		if k := types2.KindOf(obj.Type()).String(); k != "" {
			detail = strings.TrimSpace(compat.Concat(k, " ", detail))
		}
	}
	sym := symbol(f, spec.Name.Name, detail, kind, node, spec.Name)
	sym.Children = children
	return sym
}

func fieldSymbols(f *srcFile, list *ast.FieldList, kind int) []DocumentSymbol {
	if list == nil {
		return nil
	}
	var syms []DocumentSymbol
	for _, field := range list.List {
		for _, name := range field.Names {
			detail := ""
			if obj := f.pkg.info.Defs[name]; obj != nil {
				detail = types2.TypeString(obj.Type(), qualifier(f.pkg.types))
			}
			syms = compat.Append(syms, symbol(f, name.Name, detail, kind, field, name))
		}
		if len(field.Names) == 0 && field.Type != nil { // embedded
			if id := embeddedName(field.Type); id != nil {
				syms = compat.Append(syms, symbol(f, id.Name, "embedded", kind, field, id))
			}
		}
	}
	return syms
}

// embeddedName returns the type name of an embedded field.
func embeddedName(e ast.Expr) *ast.Ident {
	for {
		switch t := e.(type) {
		case *ast.Ident:
			return t
		case *ast.StarExpr:
			e = t.X
		case *ast.SelectorExpr:
			return t.Sel
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		default:
			return nil
		}
	}
}

// recvString returns a receiver type as gopls shows it: T or *T, without
// type parameters.
func recvString(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		return compat.Concat("*", recvString(star.X))
	}
	if id := embeddedName(e); id != nil {
		return id.Name
	}
	return "?"
}

func symbol(f *srcFile, name, detail string, kind int, node ast.Node, id *ast.Ident) DocumentSymbol {
	start, end := f.tok.Offset(node.Pos()), f.tok.Offset(node.End())
	if gd, ok := node.(*ast.GenDecl); ok && gd.Doc != nil {
		start = f.tok.Offset(gd.Pos())
	}
	idStart := f.tok.Offset(id.Pos())
	return DocumentSymbol{
		Name:           name,
		Detail:         detail,
		Kind:           kind,
		Range:          f.rangeOf(start, end),
		SelectionRange: f.rangeOf(idStart, idStart+len(id.Name)),
	}
}

// maxWorkspaceSymbols bounds a workspace symbol result.
const maxWorkspaceSymbols = 500

func (s *server) workspaceSymbols(p *WorkspaceSymbolParams) ([]SymbolInformation, *rpcError) {
	query := strings.ToLower(p.Query)
	result := compat.Clone([]SymbolInformation{})
	for _, pkg := range s.ws.workspacePackages() {
		for _, f := range pkg.files {
			for _, sym := range fileSymbols(f) {
				addSymbol(&result, query, pkg.path, f.path, sym)
				container := compat.Concat(pkg.path, ".", sym.Name)
				for _, c := range sym.Children {
					addSymbol(&result, query, container, f.path, c)
				}
			}
		}
	}
	return result, nil
}

// addSymbol adds sym to a workspace symbol result if it matches the query
// and the result is not full.
func addSymbol(result *[]SymbolInformation, query, container, path string, sym DocumentSymbol) {
	if len(*result) < maxWorkspaceSymbols && fuzzyMatch(query, strings.ToLower(sym.Name)) {
		*result = compat.Append(*result, SymbolInformation{
			Name: sym.Name, Kind: sym.Kind, ContainerName: container,
			Location: Location{URI: uriFromPath(path), Range: sym.SelectionRange},
		})
	}
}

// fuzzyMatch reports whether the characters of query appear in name in
// order.
func fuzzyMatch(query, name string) bool {
	for _, r := range query {
		i := strings.IndexRune(name, r)
		if i < 0 {
			return false
		}
		name = strings.SliceFrom(name, i+len(compat.RuneString(rune(r))))
	}
	return true
}
