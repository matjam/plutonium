package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
)

// A reducer shrinks a failing program while its failure signature stays
// the same and the program remains valid, deterministic Go.
type reducer struct {
	r     *runner
	sig   string
	tests int
	logf  func(format string, args ...any)
}

func (rd *reducer) interesting(src string) bool {
	rd.tests++
	v := rd.r.check(src, rd.sig)
	return v.genBug == "" && v.signature() == rd.sig
}

// reduce returns the smallest variant of src it finds.
func (rd *reducer) reduce(src string) string {
	if s, ok := repair(src); ok {
		src = s
	}
	for round := 0; round < 8; round++ {
		before := len(src)
		src = rd.removePass(src)
		src = rd.mutatePass(src)
		if len(src) >= before {
			break
		}
	}
	return src
}

// removePass deletes statements, case clauses and declarations, first in
// large chunks and then one at a time.
func (rd *reducer) removePass(src string) string {
	n := len(removable(mustParse(src)))
	removed := map[int]bool{}
	for chunk := max(n/2, 1); ; chunk /= 2 {
		for i := 0; i < n; i += chunk {
			try := map[int]bool{}
			for k := range removed {
				try[k] = true
			}
			added := false
			for k := i; k < i+chunk && k < n; k++ {
				if !try[k] {
					try[k] = true
					added = true
				}
			}
			if !added {
				continue
			}
			cand, ok := repair(removeItems(src, try))
			if ok && rd.interesting(cand) {
				removed = try
				rd.logf("reduce: removed %d of %d items (%d tests)", len(removed), n, rd.tests)
			}
		}
		if chunk == 1 {
			break
		}
	}
	if len(removed) == 0 {
		return src
	}
	out, _ := repair(removeItems(src, removed))
	return out
}

// mutatePass tries structural simplifications one at a time.
func (rd *reducer) mutatePass(src string) string {
	for k := 0; ; k++ {
		muts := mutations(src)
		if k >= len(muts) {
			return src
		}
		cand, ok := repair(muts[k]())
		if ok && cand != src && len(cand) <= len(src) && rd.interesting(cand) {
			src = cand
			k = -1 // the mutation list changed; start again
			rd.logf("reduce: simplified to %d bytes (%d tests)", len(src), rd.tests)
		}
	}
}

// ----------------------------------------------------------------------------
// Parsing, printing and repair

func parse(src string) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments|parser.SkipObjectResolution)
	return fset, f, err
}

func mustParse(src string) *ast.File {
	_, f, err := parse(src)
	if err != nil {
		panic(err)
	}
	return f
}

func render(fset *token.FileSet, f *ast.File) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return ""
	}
	return buf.String()
}

// typeErrors type-checks src as Go and returns its errors.
func typeErrors(src string) ([]types.Error, error) {
	fset, f, err := parse(src)
	if err != nil {
		return nil, err
	}
	var errs []types.Error
	conf := types.Config{
		Importer:  importer.Default(),
		GoVersion: "go1.22",
		Error:     func(err error) { errs = append(errs, err.(types.Error)) },
	}
	_, _ = conf.Check("main", fset, []*ast.File{f}, nil)
	return errs, nil
}

// repair formats src and fixes the errors deleting code typically causes
// (unused variables and labels). It reports whether the result
// type-checks.
func repair(src string) (string, bool) {
	for attempt := 0; attempt < 4; attempt++ {
		errs, err := typeErrors(src)
		if err != nil {
			return src, false
		}
		if len(errs) == 0 {
			fset, f, _ := parse(src)
			return render(fset, f), true
		}
		fset, f, _ := parse(src)
		fixed := false
		for _, e := range errs {
			switch {
			case strings.HasPrefix(e.Msg, "declared and not used: "):
				name := strings.TrimPrefix(e.Msg, "declared and not used: ")
				fixed = insertUse(f, e.Pos, name) || fixed
			case strings.HasSuffix(e.Msg, " declared and not used"):
				name := strings.TrimSuffix(e.Msg, " declared and not used")
				fixed = insertUse(f, e.Pos, name) || fixed
			case strings.HasPrefix(e.Msg, "label ") && strings.HasSuffix(e.Msg, "defined and not used"):
				fixed = dropLabel(f, e.Pos) || fixed
			default:
				return src, false
			}
		}
		if !fixed {
			return src, false
		}
		src = render(fset, f)
	}
	return src, false
}

// insertUse adds "_ = name" after the declaration of name at pos.
func insertUse(f *ast.File, pos token.Pos, name string) bool {
	use := &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{ast.NewIdent(name)}}
	done := false
	declares := func(s ast.Stmt) bool {
		switch s := s.(type) {
		case *ast.AssignStmt:
			for _, l := range s.Lhs {
				if l.Pos() == pos {
					return true
				}
			}
		case *ast.DeclStmt:
			if gd, ok := s.Decl.(*ast.GenDecl); ok {
				for _, sp := range gd.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok {
						for _, n := range vs.Names {
							if n.Pos() == pos {
								return true
							}
						}
					}
				}
			}
		}
		return false
	}
	fixList := func(list []ast.Stmt) []ast.Stmt {
		for i, s := range list {
			if declares(s) {
				done = true
				return append(list[:i+1], append([]ast.Stmt{use}, list[i+1:]...)...)
			}
		}
		return list
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if done {
			return false
		}
		switch n := n.(type) {
		case *ast.BlockStmt:
			n.List = fixList(n.List)
		case *ast.CaseClause:
			n.Body = fixList(n.Body)
		case *ast.RangeStmt:
			if (n.Key != nil && n.Key.Pos() == pos) || (n.Value != nil && n.Value.Pos() == pos) {
				n.Body.List = append([]ast.Stmt{use}, n.Body.List...)
				done = true
			}
		case *ast.IfStmt:
			if n.Init != nil && declares(n.Init) {
				n.Body.List = append([]ast.Stmt{use}, n.Body.List...)
				done = true
			}
		case *ast.ForStmt:
			if n.Init != nil && declares(n.Init) {
				n.Body.List = append([]ast.Stmt{use}, n.Body.List...)
				done = true
			}
		}
		return !done
	})
	return done
}

func dropLabel(f *ast.File, pos token.Pos) bool {
	done := false
	fix := func(list []ast.Stmt) {
		for i, s := range list {
			if l, ok := s.(*ast.LabeledStmt); ok && l.Label.Pos() == pos {
				list[i] = l.Stmt
				done = true
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			fix(n.List)
		case *ast.CaseClause:
			fix(n.Body)
		}
		return !done
	})
	return done
}

// ----------------------------------------------------------------------------
// Removal

// removable lists, in a fixed order, the nodes the reducer may delete:
// declarations other than main, statements and case clauses.
func removable(f *ast.File) []ast.Node {
	var out []ast.Node
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "main" {
			continue
		}
		out = append(out, d)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			for _, s := range n.List {
				out = append(out, s)
			}
		case *ast.CaseClause:
			for _, s := range n.Body {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// removeItems deletes the removable nodes whose indices are in set.
func removeItems(src string, set map[int]bool) string {
	fset, f, err := parse(src)
	if err != nil {
		return src
	}
	items := removable(f)
	gone := map[ast.Node]bool{}
	for i, n := range items {
		if set[i] {
			gone[n] = true
		}
	}
	filter := func(list []ast.Stmt) []ast.Stmt {
		var out []ast.Stmt
		for _, s := range list {
			if !gone[s] {
				out = append(out, s)
			}
		}
		return out
	}
	var decls []ast.Decl
	dropped := map[*ast.CommentGroup]bool{}
	for _, d := range f.Decls {
		if gone[d] {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Doc != nil {
				dropped[fd.Doc] = true
			}
			continue
		}
		decls = append(decls, d)
	}
	f.Decls = decls
	var comments []*ast.CommentGroup
	for _, c := range f.Comments {
		if !dropped[c] {
			comments = append(comments, c)
		}
	}
	f.Comments = comments
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			n.List = filter(n.List)
		case *ast.CaseClause:
			n.Body = filter(n.Body)
		}
		return true
	})
	return render(fset, f)
}

// ----------------------------------------------------------------------------
// Mutations

// mutations lists simplifications of src, each producing a new source.
func mutations(src string) []func() string {
	var out []func() string
	// Statement shapes: splice a compound statement's body into its
	// block, or drop an else branch.
	f := mustParse(src)
	nstmt := 0
	ast.Inspect(f, func(n ast.Node) bool {
		var list []ast.Stmt
		switch n := n.(type) {
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		}
		for _, s := range list {
			k := nstmt
			nstmt++
			switch s := s.(type) {
			case *ast.IfStmt:
				out = append(out, func() string { return spliceStmt(src, k, 0) })
				if s.Else != nil {
					out = append(out, func() string { return spliceStmt(src, k, 1) }, func() string { return spliceStmt(src, k, 2) })
				}
			case *ast.ForStmt, *ast.RangeStmt, *ast.BlockStmt, *ast.LabeledStmt:
				out = append(out, func() string { return spliceStmt(src, k, 0) })
			}
		}
		return true
	})
	// Expressions: replace one by an operand or a zero constant.
	nexpr := 0
	walkExprs(mustParse(src), func(e ast.Expr) ast.Expr {
		k := nexpr
		nexpr++
		for v := 0; v < 4; v++ {
			if exprVariant(e, v) != nil {
				out = append(out, func() string { return replaceExpr(src, k, v) })
			}
		}
		return e
	})
	return out
}

// spliceStmt replaces the k-th statement (in list order) by its body
// (variant 0), drops its else branch (1) or replaces it by its else
// branch (2).
func spliceStmt(src string, k, variant int) string {
	fset, f, err := parse(src)
	if err != nil {
		return src
	}
	nstmt := 0
	splice := func(list []ast.Stmt) []ast.Stmt {
		var out []ast.Stmt
		for _, s := range list {
			if nstmt != k {
				nstmt++
				out = append(out, s)
				continue
			}
			nstmt++
			if l, ok := s.(*ast.LabeledStmt); ok {
				out = append(out, l.Stmt)
				continue
			}
			switch s := s.(type) {
			case *ast.IfStmt:
				switch variant {
				case 0:
					if s.Init != nil {
						out = append(out, s.Init)
					}
					out = append(out, s.Body.List...)
				case 1:
					s.Else = nil
					out = append(out, s)
				default:
					if b, ok := s.Else.(*ast.BlockStmt); ok {
						out = append(out, b.List...)
					} else {
						out = append(out, s.Else)
					}
				}
			case *ast.ForStmt:
				out = append(out, s.Body.List...)
			case *ast.RangeStmt:
				out = append(out, s.Body.List...)
			case *ast.BlockStmt:
				out = append(out, s.List...)
			default:
				out = append(out, s)
			}
		}
		return out
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			n.List = splice(n.List)
		case *ast.CaseClause:
			n.Body = splice(n.Body)
		}
		return true
	})
	return render(fset, f)
}

// exprVariant returns a simpler replacement for e, or nil.
func exprVariant(e ast.Expr, v int) ast.Expr {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		switch v {
		case 0:
			return e.X
		case 1:
			return e.Y
		}
	case *ast.ParenExpr:
		if v == 0 {
			return e.X
		}
	case *ast.UnaryExpr:
		if v == 0 {
			return e.X
		}
	case *ast.CallExpr:
		if v < len(e.Args) && v < 3 {
			return e.Args[v]
		}
	case *ast.BasicLit:
		return nil
	case *ast.Ident:
		if e.Name == "_" || e.Name == "true" || e.Name == "false" {
			return nil
		}
	}
	if v == 3 {
		return &ast.BasicLit{Kind: token.INT, Value: "0"}
	}
	return nil
}

func replaceExpr(src string, k, v int) string {
	fset, f, err := parse(src)
	if err != nil {
		return src
	}
	n := 0
	walkExprs(f, func(e ast.Expr) ast.Expr {
		i := n
		n++
		if i == k {
			if r := exprVariant(e, v); r != nil {
				return r
			}
		}
		return e
	})
	return render(fset, f)
}

var (
	exprType  = reflect.TypeOf((*ast.Expr)(nil)).Elem()
	nodeType  = reflect.TypeOf((*ast.Node)(nil)).Elem()
	identType = reflect.TypeOf((*ast.Ident)(nil))
)

// walkExprs calls fn on every expression in function bodies, in a fixed
// order, replacing each by fn's result. It does not descend into a
// replaced expression, nor into the left side of assignments,
// declarations or types.
func walkExprs(f *ast.File, fn func(ast.Expr) ast.Expr) {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			walkValue(reflect.ValueOf(fd.Body), fn)
		}
	}
}

func walkValue(v reflect.Value, fn func(ast.Expr) ast.Expr) {
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	switch n := v.Interface().(type) {
	case *ast.AssignStmt:
		walkSlice(reflect.ValueOf(n.Rhs), fn)
		for _, l := range n.Lhs {
			walkIndexes(l, fn)
		}
		return
	case *ast.ValueSpec:
		walkSlice(reflect.ValueOf(n.Values), fn)
		return
	case *ast.FuncLit:
		walkValue(reflect.ValueOf(n.Body), fn)
		return
	case *ast.CompositeLit:
		walkSlice(reflect.ValueOf(n.Elts), fn)
		return
	case *ast.KeyValueExpr:
		walkField(reflect.ValueOf(n).Elem().FieldByName("Value"), fn)
		return
	case *ast.RangeStmt:
		walkField(reflect.ValueOf(n).Elem().FieldByName("X"), fn)
		walkValue(reflect.ValueOf(n.Body), fn)
		return
	case *ast.IncDecStmt, *ast.BranchStmt, *ast.LabeledStmt:
		if l, ok := n.(*ast.LabeledStmt); ok {
			walkValue(reflect.ValueOf(l.Stmt), fn)
		}
		return
	case *ast.SelectorExpr:
		walkField(reflect.ValueOf(n).Elem().FieldByName("X"), fn)
		return
	case *ast.CaseClause:
		walkSlice(reflect.ValueOf(n.Body), fn)
		return
	case *ast.CallExpr:
		walkSlice(reflect.ValueOf(n.Args), fn)
		return
	}
	s := v.Elem()
	if s.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < s.NumField(); i++ {
		walkField(s.Field(i), fn)
	}
}

// walkIndexes visits the index expressions of an assignment target.
func walkIndexes(e ast.Expr, fn func(ast.Expr) ast.Expr) {
	switch e := e.(type) {
	case *ast.IndexExpr:
		walkIndexes(e.X, fn)
		walkField(reflect.ValueOf(e).Elem().FieldByName("Index"), fn)
	case *ast.SelectorExpr:
		walkIndexes(e.X, fn)
	}
}

func walkField(fv reflect.Value, fn func(ast.Expr) ast.Expr) {
	switch {
	case fv.Type() == exprType:
		if fv.IsNil() {
			return
		}
		e := fv.Interface().(ast.Expr)
		if r := fn(e); r != e {
			fv.Set(reflect.ValueOf(r))
			return
		}
		walkValue(fv, fn)
	case fv.Kind() == reflect.Slice:
		walkSlice(fv, fn)
	case fv.Type() == identType:
	case fv.Type().Implements(nodeType):
		walkValue(fv, fn)
	}
}

func walkSlice(sv reflect.Value, fn func(ast.Expr) ast.Expr) {
	for i := 0; i < sv.Len(); i++ {
		walkField(sv.Index(i), fn)
	}
}
