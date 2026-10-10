package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// goModule is the compiler's Go module path.
const goModule = "github.com/matjam/plutonium"

// Config controls a translation.
type Config struct {
	OutDir    string            // output root, the directory of the Plutonium module
	Module    string            // Plutonium module path
	PlutoRoot string            // standard library root ("lib"), for the names that exist
	Imports   map[string]string // import path replacements, applied first
}

// A Result is one translated package.
type Result struct {
	GoPath  string // Go import path
	OutPath string // Plutonium import path
	Files   []OutFile
	Notes   []string // constructs left for a person to fix, as "file:line: message"
}

// An OutFile is one translated source file.
type OutFile struct {
	Name string // base name, ending in .p
	Src  []byte
}

// Write stores the translated files under root, the directory of the
// Plutonium module.
func (r *Result) Write(root string) error {
	dir, err := r.Dir(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range r.Files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Src, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Dir returns the directory under root that holds the package.
func (r *Result) Dir(root string) (string, error) {
	_, rest, ok := strings.Cut(r.OutPath, "/")
	if !ok {
		return "", fmt.Errorf("%s: output path %q is not inside a module", r.GoPath, r.OutPath)
	}
	return filepath.Join(root, filepath.FromSlash(rest)), nil
}

// stdMap gives the Plutonium package for each Go standard library package
// the compiler imports. An empty value means calls into the package are
// rewritten individually (see special) and the import disappears.
var stdMap = map[string]string{
	"bytes":           "pluto/bytes",
	"errors":          "pluto/errors",
	"fmt":             "pluto/fmt",
	"io":              "pluto/io",
	"math":            "pluto/math",
	"math/bits":       "pluto/bits",
	"os":              "pluto/os",
	"sort":            "pluto/sort",
	"strconv":         "pluto/strconv",
	"strings":         "pluto/strings",
	"unicode/utf8":    "pluto/unicode/utf8",
	"unsafe":          "unsafe",
	"encoding/binary": "",
	"debug/elf":       "$MOD/elf",
}

// special replaces qualified identifiers of Go packages with compat ones.
var special = map[string]string{
	"fmt.Sprintf":     "compat.Sprintf",
	"fmt.Sprint":      "compat.Sprint",
	"fmt.Sprintln":    "compat.Sprintln",
	"fmt.Errorf":      "compat.Errorf",
	"strings.Builder": "compat.Builder",
	"strings.Join":    "compat.Join",
	"bytes.Buffer":    "compat.Buffer",
	"errors.Is":       "compat.Is",
	"errors.Unwrap":   "compat.Unwrap",
}

const (
	compatPkg = "$MOD/compat"
	hashPkg   = "pluto/hash"
	ioPkg     = "pluto/io"
	sortPkg   = "pluto/sort"
)

// TranslateDir translates the Go package in dir.
func TranslateDir(cfg *Config, dir string) (*Result, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	modRoot, modPath, err := findGoMod(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(modRoot, abs)
	if err != nil {
		return nil, err
	}
	goPath := modPath
	if rel != "." {
		goPath = modPath + "/" + filepath.ToSlash(rel)
	}

	fset := token.NewFileSet()
	names, err := goFiles(abs)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	srcs := map[*ast.File][]byte{}
	for _, name := range names {
		src, err := os.ReadFile(filepath.Join(abs, name))
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, filepath.Join(abs, name), src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		srcs[f] = src
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check(goPath, fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("type-checking %s: %w", goPath, err)
	}

	esc, err := goEscapes(abs, filepath.ToSlash(rel))
	if err != nil {
		return nil, err
	}
	res := &Result{GoPath: goPath}
	tc := &translator{cfg: cfg, fset: fset, info: info, pkg: pkg, esc: esc, exports: plutoExports(cfg.PlutoRoot)}
	res.OutPath = tc.mapPath(goPath)
	for _, f := range files {
		tf := &fileTr{translator: tc, file: f, src: srcs[f], res: res}
		out, err := tf.translate()
		if err != nil {
			return nil, err
		}
		base := filepath.Base(fset.File(f.Pos()).Name())
		res.Files = append(res.Files, OutFile{Name: strings.TrimSuffix(base, ".go") + ".p", Src: out})
	}
	sort.Strings(res.Notes)
	return res, nil
}

// findGoMod returns the directory and module path of the go.mod above dir.
func findGoMod(dir string) (string, string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
					return d, strings.TrimSpace(rest), nil
				}
			}
			return "", "", fmt.Errorf("%s/go.mod has no module line", d)
		}
		if filepath.Dir(d) == d {
			return "", "", fmt.Errorf("%s is not inside a Go module", dir)
		}
	}
}

// goFiles lists the non-test Go files of dir that match the build context.
func goFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if ok, err := build.Default.MatchFile(dir, name); err != nil || !ok {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, errors.New(dir + ": no Go files")
	}
	return names, nil
}

// A translator holds what the files of one package share.
type translator struct {
	cfg     *Config
	fset    *token.FileSet
	info    *types.Info
	pkg     *types.Package
	esc     escapes
	exports map[string]map[string]bool
}

// mapPath returns the Plutonium import path for a Go import path, or "" if
// the package has no counterpart.
func (tc *translator) mapPath(p string) string {
	if to, ok := tc.cfg.Imports[p]; ok {
		return to
	}
	if p == goModule {
		return tc.cfg.Module
	}
	if rest, ok := strings.CutPrefix(p, goModule+"/"); ok {
		return tc.cfg.Module + "/" + rest
	}
	if to, ok := stdMap[p]; ok {
		return strings.ReplaceAll(to, "$MOD", tc.cfg.Module)
	}
	return ""
}

// fileTr translates one file.
type fileTr struct {
	*translator
	file   *ast.File
	src    []byte
	res    *Result
	parent map[ast.Node]ast.Node

	local   map[string]string // Go import path -> local name in this file
	needed  map[string]string // Plutonium import path -> local name
	subst   map[ast.Node]string
	extra   []string        // generated declarations
	gen     map[string]bool // generated declaration names
	counter int
	noted   map[string]bool
}

func (t *fileTr) translate() ([]byte, error) {
	t.local = map[string]string{}
	t.needed = map[string]string{}
	t.subst = map[ast.Node]string{}
	t.gen = map[string]bool{}
	t.noted = map[string]bool{}
	t.parent = map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(t.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			t.parent[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	for _, imp := range t.file.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		name := path.Base(p)
		if pk, ok := t.info.Implicits[imp].(*types.PkgName); ok {
			name = pk.Name()
		}
		if imp.Name != nil {
			name = imp.Name.Name
		}
		t.local[p] = name
		if t.mapPath(p) == "" && stdMap[p] != "" {
			t.note(imp, "no Plutonium package for import %q", p)
		}
		if _, known := stdMap[p]; !known && !strings.HasPrefix(p, goModule) && t.cfg.Imports[p] == "" {
			t.note(imp, "no Plutonium package for import %q", p)
		}
	}

	base := filepath.Base(t.fset.File(t.file.Pos()).Name())
	var moved []string
	for key, msgs := range t.esc {
		for _, m := range msgs {
			if name, ok := strings.CutPrefix(m, "moved to heap: "); ok && strings.HasPrefix(key, base+":") {
				moved = append(moved, fmt.Sprintf("%s: variable %s outlives its frame in Go (moved to heap): check that its address is not kept", key, name))
			}
		}
	}
	sort.Strings(moved)
	for _, m := range moved {
		if !t.noted[m] {
			t.noted[m] = true
			t.res.Notes = append(t.res.Notes, m)
		}
	}

	body := t.render(t.file)
	if !strings.Contains(body, importMarker) {
		// No import declaration to replace: put one after the package
		// clause.
		at := t.off(t.file.Name.End()) - t.off(t.file.Pos())
		body = body[:at] + "\n\n" + importMarker + body[at:]
	}
	out := new(bytes.Buffer)
	rel := strings.TrimPrefix(t.res.GoPath, goModule+"/") + "/" + base
	fmt.Fprintf(out, "// Code generated by go2pu from %s. DO NOT EDIT.\n\n", rel)
	// The package clause and what precedes it (the package comment).
	out.Write(t.src[:t.off(t.file.Pos())])
	out.WriteString(strings.Replace(body, importMarker, t.importBlock(), 1))
	for _, d := range t.extra {
		out.WriteString("\n")
		out.WriteString(d)
	}
	src, err := format.Source(out.Bytes())
	if err != nil {
		return out.Bytes(), fmt.Errorf("%s: formatting the translation: %w", rel, err)
	}
	return src, nil
}

const importMarker = "\x00imports\x00"

// importBlock returns the import declaration of the translated file.
func (t *fileTr) importBlock() string {
	if len(t.needed) == 0 {
		return ""
	}
	var paths []string
	for p := range t.needed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	b.WriteString("import (\n")
	for _, p := range paths {
		name := t.needed[p]
		if name == path.Base(p) {
			fmt.Fprintf(&b, "\t%q\n", p)
		} else {
			fmt.Fprintf(&b, "\t%s %q\n", name, p)
		}
	}
	b.WriteString(")\n")
	return b.String()
}

// use records that the translation refers to package p as name.
func (t *fileTr) use(p, name string) string {
	p = strings.ReplaceAll(p, "$MOD", t.cfg.Module)
	t.needed[p] = name
	return name
}

func (t *fileTr) compat() string { return t.use(compatPkg, "compat") }

// note records a construct the translator left for a person.
func (t *fileTr) note(n ast.Node, format string, args ...any) {
	pos := t.fset.Position(n.Pos())
	msg := fmt.Sprintf("%s:%d: %s", filepath.Base(pos.Filename), pos.Line, fmt.Sprintf(format, args...))
	if !t.noted[msg] {
		t.noted[msg] = true
		t.res.Notes = append(t.res.Notes, msg)
	}
}

func (t *fileTr) fresh(prefix string) string {
	t.counter++
	return fmt.Sprintf("%s%d", prefix, t.counter)
}

// text returns the source of n.
func (t *fileTr) text(n ast.Node) string {
	return string(t.src[t.off(n.Pos()):t.off(n.End())])
}

func (t *fileTr) off(p token.Pos) int { return t.fset.File(p).Offset(p) }

// render returns n translated.
func (t *fileTr) render(n ast.Node) string {
	if n == nil {
		return ""
	}
	if s, ok := t.rewrite(n); ok {
		return s
	}
	return t.splice(n)
}

// splice returns n's source with its outermost rewritten descendants
// replaced by their translations.
func (t *fileTr) splice(n ast.Node) string {
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	ast.Inspect(n, func(c ast.Node) bool {
		if c == nil || c == n {
			return true
		}
		if s, ok := t.rewrite(c); ok {
			edits = append(edits, edit{t.off(c.Pos()), t.off(c.End()), s})
			return false
		}
		return true
	})
	start := t.off(n.Pos())
	var b strings.Builder
	at := start
	for _, e := range edits {
		if e.start < at {
			continue // nested inside an earlier edit (comments)
		}
		b.Write(t.src[at:e.start])
		b.WriteString(e.text)
		at = e.end
	}
	b.Write(t.src[at:t.off(n.End())])
	return b.String()
}

// list renders expressions separated by commas.
func (t *fileTr) list(es []ast.Expr) string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = t.render(e)
	}
	return strings.Join(parts, ", ")
}

// typeOf returns the type of e, or nil.
func (t *fileTr) typeOf(e ast.Expr) types.Type { return t.info.TypeOf(e) }

// isMapValue reports whether e is a value of map type.
func (t *fileTr) isMapValue(e ast.Expr) bool {
	tv, ok := t.info.Types[e]
	if !ok || !tv.IsValue() {
		return false
	}
	_, isMap := tv.Type.Underlying().(*types.Map)
	return isMap
}

func isString(T types.Type) bool {
	b, ok := T.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func elemBasic(T types.Type) types.BasicKind {
	s, ok := T.Underlying().(*types.Slice)
	if !ok {
		return types.Invalid
	}
	if b, ok := s.Elem().Underlying().(*types.Basic); ok {
		return b.Kind()
	}
	return types.Invalid
}
