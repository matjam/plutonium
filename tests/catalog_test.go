package tests

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The tests in this file check the diagnostic catalog of the compiler
// (compiler/internal/diag) without building or running it. Plutonium's
// syntax is close enough to Go's that go/parser reads the catalog's
// sources, so the tests read the entries from the declarations and rebuild
// the catalog the way the package's init functions do.

var (
	diagDir   = filepath.Join("..", "compiler", "internal", "diag")
	typerrDir = filepath.Join("..", "compiler", "internal", "types2", "errors")
)

// A catalogEntry is an entry of the catalog, as the Entry the compiler
// registers: its code and the text pluto explain prints.
type catalogEntry struct {
	Code, Name                      string
	Text, Why, Spec, Example, Fixed string
	FixIntro, FixCode               string
	pos                             string // where the entry is declared
}

// A catalog is the catalog read from the sources, with the codes it is
// checked against.
type catalog struct {
	entries map[string]*catalogEntry
	// checker maps the type checker's code names (types2/errors) to their
	// values; diag maps the names of the codes in diag/codes.p to theirs.
	checker map[string]int64
	diag    map[string]string
}

// codes returns the catalog's codes in order.
func (c *catalog) codes() []string {
	codes := make([]string, 0, len(c.entries))
	for code := range c.entries {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// checkerCode is diag.CheckerCode: the code of the type checker's error
// code with value v.
func checkerCode(v int64) string {
	return fmt.Sprintf("E%04d", v)
}

// parseDir parses the .p files of dir, without the ones named in skip.
func parseDir(t *testing.T, fset *token.FileSet, dir string, skip ...string) []*ast.File {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, name := range names {
		skipped := false
		for _, s := range skip {
			skipped = skipped || filepath.Base(name) == s
		}
		if skipped {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("go/parser cannot read %s: %v", name, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatalf("no .p files in %s", dir)
	}
	return files
}

// checkerCodes returns the type checker's error codes by name, with their
// values. codes.p and plutonium.p are plain Go, so go/types computes the
// values of their iota constants. code_string.p imports Plutonium
// packages and is left out.
func checkerCodes(t *testing.T) map[string]int64 {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range []string{"codes.p", "plutonium.p"} {
		f, err := parser.ParseFile(fset, filepath.Join(typerrDir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	conf := types.Config{Error: func(err error) { t.Errorf("type checking types2/errors: %v", err) }}
	pkg, _ := conf.Check("errors", fset, files, nil)
	if pkg == nil {
		t.FailNow()
	}
	codes := map[string]int64{}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if !ok || name == "Test" {
			continue
		}
		if named, ok := c.Type().(*types.Named); !ok || named.Obj().Name() != "Code" {
			continue
		}
		v, _ := constant.Int64Val(c.Val())
		codes[name] = v
	}
	return codes
}

// readCatalog reads the catalog from the sources of compiler/internal/diag.
func readCatalog(t *testing.T) *catalog {
	t.Helper()
	fset := token.NewFileSet()
	files := parseDir(t, fset, diagDir)
	r := &catalogReader{t: t, fset: fset, consts: map[string]ast.Expr{}, vars: map[string]ast.Expr{}}
	c := &catalog{entries: map[string]*catalogEntry{}, checker: checkerCodes(t), diag: map[string]string{}}
	r.cat = c

	// The package-level constants and variables, by name.
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if gd.Tok == token.CONST {
						r.consts[id.Name] = vs.Values[i]
					} else {
						r.vars[id.Name] = vs.Values[i]
					}
				}
			}
		}
	}

	// The codes of codes.p are the constants of type Code.
	for _, f := range files {
		if filepath.Base(fset.Position(f.Pos()).Filename) != "codes.p" {
			continue
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				for i, id := range vs.Names {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: code %s is not a string literal", fset.Position(id.Pos()), id.Name)
					}
					v, _ := strconv.Unquote(lit.Value)
					c.diag[id.Name] = v
				}
			}
		}
	}
	if len(c.diag) == 0 {
		t.Fatal("no codes in diag/codes.p")
	}

	// Each init function registers entries, each written as
	// pu.New(Entry{...}). catalog_types.p registers the type checker's
	// codes from goDocs, which goEntries rebuilds.
	sawGoDocs := false
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "init" || fd.Recv != nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isIdent(call.Fun, "register") {
					return true
				}
				for _, arg := range call.Args {
					if id, ok := arg.(*ast.Ident); ok && id.Name == "e" {
						sawGoDocs = true // the loop over goDocs in catalog_types.p
						continue
					}
					lit := r.entryLit(arg)
					if lit == nil {
						t.Errorf("%s: register's argument is not pu.New(Entry{...})", fset.Position(arg.Pos()))
						continue
					}
					r.add(r.entry(lit))
				}
				return false
			})
		}
	}
	if !sawGoDocs {
		t.Fatal("no init registers the go/types codes (register(e) in catalog_types.p); update readCatalog")
	}
	r.goEntries()
	return c
}

// A catalogReader evaluates the declarations of the catalog.
type catalogReader struct {
	t      *testing.T
	fset   *token.FileSet
	cat    *catalog
	consts map[string]ast.Expr
	vars   map[string]ast.Expr
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// isSel reports whether e is pkg.name.
func isSel(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && isIdent(sel.X, pkg) && sel.Sel.Name == name
}

func (r *catalogReader) pos(n ast.Node) string {
	p := r.fset.Position(n.Pos())
	return fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
}

// entryLit returns the literal of pu.New(Entry{...}), or nil.
func (r *catalogReader) entryLit(e ast.Expr) *ast.CompositeLit {
	call, ok := e.(*ast.CallExpr)
	if !ok || !isSel(call.Fun, "pu", "New") || len(call.Args) != 1 {
		return nil
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok || !isIdent(lit.Type, "Entry") {
		return nil
	}
	return lit
}

// add registers e, as register does.
func (r *catalogReader) add(e *catalogEntry) {
	if prev, dup := r.cat.entries[e.Code]; dup {
		r.t.Errorf("%s: duplicate catalog entry %s (first at %s)", e.pos, e.Code, prev.pos)
		return
	}
	r.cat.entries[e.Code] = e
}

// entry evaluates an Entry literal. The literal of goPlain has no type,
// so lit.Type is not checked here.
func (r *catalogReader) entry(lit *ast.CompositeLit) *catalogEntry {
	e := &catalogEntry{pos: r.pos(lit)}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			r.t.Errorf("%s: Entry literal without field names", r.pos(elt))
			continue
		}
		key, _ := kv.Key.(*ast.Ident)
		if key == nil {
			r.t.Errorf("%s: unexpected key in Entry literal", r.pos(kv))
			continue
		}
		switch key.Name {
		case "Code":
			e.Code = r.code(kv.Value)
		case "Name":
			e.Name = r.str(kv.Value)
		case "Text":
			e.Text = r.str(kv.Value)
		case "Why":
			e.Why = r.str(kv.Value)
		case "Spec":
			e.Spec = r.str(kv.Value)
		case "Example":
			e.Example = r.str(kv.Value)
		case "Fixed":
			e.Fixed = r.str(kv.Value)
		case "Fix":
			e.FixIntro, e.FixCode = r.fix(kv.Value)
		default:
			r.t.Errorf("%s: Entry field %s is not known to this test; update catalogEntry", r.pos(kv), key.Name)
		}
	}
	return e
}

// fix evaluates pu.New(Fix{Intro: ..., Code: ...}).
func (r *catalogReader) fix(e ast.Expr) (intro, code string) {
	call, ok := e.(*ast.CallExpr)
	var lit *ast.CompositeLit
	if ok && isSel(call.Fun, "pu", "New") && len(call.Args) == 1 {
		lit, _ = call.Args[0].(*ast.CompositeLit)
	}
	if lit == nil || !isIdent(lit.Type, "Fix") {
		r.t.Errorf("%s: Fix is not pu.New(Fix{...})", r.pos(e))
		return "", ""
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			r.t.Errorf("%s: Fix literal without field names", r.pos(elt))
			continue
		}
		switch {
		case isIdent(kv.Key, "Intro"):
			intro = r.str(kv.Value)
		case isIdent(kv.Key, "Code"):
			code = r.str(kv.Value)
		default:
			r.t.Errorf("%s: unknown Fix field", r.pos(kv))
		}
	}
	return intro, code
}

// code evaluates a code: a constant of codes.p, or
// CheckerCode(typerr.Name).
func (r *catalogReader) code(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		if v, ok := r.cat.diag[e.Name]; ok {
			return v
		}
	case *ast.CallExpr:
		if isIdent(e.Fun, "CheckerCode") && len(e.Args) == 1 {
			if sel, ok := e.Args[0].(*ast.SelectorExpr); ok && isIdent(sel.X, "typerr") {
				if v, ok := r.cat.checker[sel.Sel.Name]; ok {
					return checkerCode(v)
				}
				r.t.Errorf("%s: types2/errors has no code %s", r.pos(e), sel.Sel.Name)
				return ""
			}
		}
	}
	r.t.Errorf("%s: cannot evaluate the code %s", r.pos(e), r.src(e))
	return ""
}

// str evaluates a string: a literal, a constant, a sum of strings, or a
// call of pspec or compat.Concat.
func (r *catalogReader) str(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			if err != nil {
				r.t.Errorf("%s: %v", r.pos(e), err)
			}
			return s
		}
	case *ast.ParenExpr:
		return r.str(e.X)
	case *ast.Ident:
		if v, ok := r.consts[e.Name]; ok {
			return r.str(v)
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return r.str(e.X) + r.str(e.Y)
		}
	case *ast.CallExpr:
		switch {
		case isIdent(e.Fun, "pspec") && len(e.Args) == 2:
			// pspec in catalog_pluto.p.
			return `"` + r.str(e.Args[1]) + `" in the Plutonium spec, section ` + r.str(e.Args[0])
		case isSel(e.Fun, "compat", "Concat"):
			var b strings.Builder
			for _, a := range e.Args {
				b.WriteString(r.str(a))
			}
			return b.String()
		}
	}
	r.t.Errorf("%s: cannot evaluate the string %s; extend catalogReader.str", r.pos(e), r.src(e))
	return ""
}

func (r *catalogReader) src(e ast.Expr) string {
	p := r.fset.Position(e.Pos())
	data, err := os.ReadFile(p.Filename)
	if err != nil {
		return "?"
	}
	end := r.fset.Position(e.End()).Offset
	if end-p.Offset > 80 {
		end = p.Offset + 80
	}
	return string(data[p.Offset:end])
}

// goEntries rebuilds the entries that catalog_types.p's init registers for
// the type checker's codes: each goDoc becomes an entry, with the text of
// goPlain where there is one and otherwise go/types' description and the
// section of goSections.
func (r *catalogReader) goEntries() {
	t := r.t
	docs, ok := r.vars["goDocs"].(*ast.CompositeLit)
	if !ok {
		t.Fatal("no var goDocs = []goDoc{...} in diag")
	}
	plain := map[string]*ast.CompositeLit{}
	if m, ok := r.vars["goPlain"].(*ast.CompositeLit); ok {
		for _, elt := range m.Elts {
			kv := elt.(*ast.KeyValueExpr)
			lit, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				t.Errorf("%s: goPlain value is not a literal", r.pos(kv))
				continue
			}
			plain[r.str(kv.Key)] = lit
		}
	} else {
		t.Fatal("no var goPlain = map[string]Entry{...} in diag")
	}
	sections := map[string]string{}
	if m, ok := r.vars["goSections"].(*ast.CompositeLit); ok {
		for _, elt := range m.Elts {
			kv := elt.(*ast.KeyValueExpr)
			sections[r.str(kv.Key)] = r.str(kv.Value)
		}
	} else {
		t.Fatal("no var goSections = map[string]string{...} in diag")
	}
	used := map[string]bool{}
	for _, elt := range docs.Elts {
		lit, ok := elt.(*ast.CompositeLit)
		if !ok {
			t.Errorf("%s: goDocs element is not a literal", r.pos(elt))
			continue
		}
		var value int64 = -1
		var name, text string
		var examples []string
		for _, f := range lit.Elts {
			kv := f.(*ast.KeyValueExpr)
			switch {
			case isIdent(kv.Key, "Value"):
				bl, ok := kv.Value.(*ast.BasicLit)
				if ok && bl.Kind == token.INT {
					value, _ = strconv.ParseInt(bl.Value, 0, 64)
				} else {
					t.Errorf("%s: Value is not an integer", r.pos(kv))
				}
			case isIdent(kv.Key, "Name"):
				name = r.str(kv.Value)
			case isIdent(kv.Key, "Text"):
				text = r.str(kv.Value)
			case isIdent(kv.Key, "Spec"):
				// A quote from Go's spec, which the catalog leaves out.
			case isIdent(kv.Key, "Examples"):
				ex, ok := kv.Value.(*ast.CompositeLit)
				if !ok {
					t.Errorf("%s: Examples is not a literal", r.pos(kv))
					continue
				}
				for _, x := range ex.Elts {
					examples = append(examples, r.str(x))
				}
			default:
				t.Errorf("%s: unknown goDoc field", r.pos(kv))
			}
		}
		if v, ok := r.cat.checker[name]; !ok || v != value {
			t.Errorf("%s: goDocs has %s = %d, the type checker %s = %d", r.pos(lit), name, value, name, v)
		}
		e := &catalogEntry{Code: checkerCode(value), Name: name, pos: r.pos(lit)}
		if p, ok := plain[name]; ok {
			used[name] = true
			pe := r.entry(p)
			e.Text, e.FixIntro, e.FixCode, e.Spec, e.Example, e.Fixed = pe.Text, pe.FixIntro, pe.FixCode, pe.Spec, pe.Example, pe.Fixed
		} else {
			// describeDoc in catalog_types.p.
			if rest, ok := strings.CutPrefix(text, name+" "); ok {
				text = "This error " + rest
			}
			e.Text = text
			e.Spec = sections[name]
		}
		if e.Example == "" && len(examples) > 0 {
			e.Example = examples[0]
		}
		r.add(e)
	}
	for name := range plain {
		if !used[name] {
			t.Errorf("goPlain has %s, which goDocs does not document", name)
		}
	}
}

// TestCatalogComplete checks that every error code, of the type checker
// and of the rest of the compiler, has an explanation under its own name,
// and that every explanation belongs to a code.
func TestCatalogComplete(t *testing.T) {
	t.Parallel()
	c := readCatalog(t)
	used := map[string]string{}
	for name, v := range c.checker {
		if name == "InvalidSyntaxTree" {
			continue // reported as InternalError
		}
		code := checkerCode(v)
		if other, dup := used[code]; dup {
			t.Errorf("%s is used by both %s and %s", code, other, name)
		}
		used[code] = name
		e := c.entries[code]
		if e == nil {
			t.Errorf("checker code %s (%s) has no catalog entry", name, code)
			continue
		}
		if e.Name != name {
			t.Errorf("%s: catalog names it %s, the checker %s", code, e.Name, name)
		}
	}
	for name, code := range c.diag {
		if other, dup := used[code]; dup {
			t.Errorf("%s is used by both %s and %s", code, other, name)
		}
		used[code] = name
		if e := c.entries[code]; e == nil {
			t.Errorf("code %s (%s) has no catalog entry", name, code)
		} else if e.Name != name {
			t.Errorf("%s: catalog names it %s, codes.p %s", code, e.Name, name)
		}
	}
	for _, code := range c.codes() {
		if _, ok := used[code]; !ok {
			t.Errorf("catalog entry %s (%s, %s) has no code", code, c.entries[code].Name, c.entries[code].pos)
		}
	}
	if len(c.entries) < 300 {
		t.Errorf("read only %d catalog entries; is the catalog read correctly?", len(c.entries))
	}
	t.Logf("%d catalog entries, %d checker codes, %d other codes", len(c.entries), len(c.checker), len(c.diag))
}

var codeForm = regexp.MustCompile(`^E\d{4}$`)

// TestCatalogStyle checks the catalog's entries against the rules for
// explanations: a code in its range, text for every entry, a reason and a
// spec section for every Plutonium rule, and plain punctuation.
func TestCatalogStyle(t *testing.T) {
	t.Parallel()
	c := readCatalog(t)
	for _, code := range c.codes() {
		e := c.entries[code]
		if !codeForm.MatchString(code) {
			t.Errorf("%s (%s): malformed code", code, e.pos)
		}
		if e.Text == "" || e.Name == "" {
			t.Errorf("%s (%s): no name or text", code, e.pos)
		}
		n, _ := strconv.Atoi(code[1:])
		if n >= 2000 && n < 4000 && e.Spec == "" {
			t.Errorf("%s (%s): a Plutonium rule needs a spec section", code, e.Name)
		}
		all := e.Text + e.Why + e.Spec + e.Example + e.Fixed + e.FixIntro + e.FixCode
		for _, bad := range []string{"—", "–", "TODO"} {
			if strings.Contains(all, bad) {
				t.Errorf("%s (%s): contains %q", code, e.Name, bad)
			}
		}
	}
	// The rules that differ from Go say why.
	for _, code := range []string{
		checkerCode(c.checker["RecoverNotPermitted"]),
		checkerCode(c.checker["ErrorsNewNotConstant"]),
		checkerCode(c.checker["MissingEnumCase"]),
		c.diag["KernelFloat"],
		c.diag["DeferInLoop"],
	} {
		if e := c.entries[code]; e == nil || e.Why == "" {
			t.Errorf("%s: no reason why Plutonium differs from Go", code)
		}
	}
}
