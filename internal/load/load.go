// Package load finds, parses and type-checks the packages of a Plutonium
// program.
//
// Import paths resolve in two places. Paths under pluto/ name standard
// library packages, read from Config.Stdlib. Any other path must lie inside
// the main package's module: the nearest enclosing directory with a pluto.mod
// file whose first line is "module <path>". Without a pluto.mod the main
// package stands alone and may import only the standard library.
package load

import (
	"bufio"
	"errors"
	"fmt"
	"go/build/constraint"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Config controls loading.
type Config struct {
	Tags   map[string]bool // build tags that are set
	Stdlib fs.FS           // root of the standard library; its top directory is "pluto"
	Sizes  types2.Sizes
}

// A Package is a loaded, type-checked package.
type Package struct {
	Path     string // import path
	Dir      string // directory, for messages
	Files    []*ast.File
	AsmFiles []AsmFile // .s files selected by build tags
	CFiles   []AsmFile // .c files selected by build tags, compiled with the system C compiler
	ObjFiles []AsmFile // .o files selected by file name tags, linked as they are
	Types    *types2.Package
	Info     *types2.Info
	Imports  []*Package
}

// An AsmFile is a file in a package that is not Plutonium source: assembly,
// C, or an object file.
type AsmFile struct {
	Name string // for messages
	Src  []byte
}

// A Program is every package reachable from a main package.
type Program struct {
	Fset     *token.FileSet
	Main     *Package
	Packages []*Package // in dependency order: every package after its imports
}

// Load loads the main package in dir and everything it imports.
func Load(cfg *Config, dir string) (*Program, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	l := &loader{
		cfg:      cfg,
		fset:     token.NewFileSet(),
		packages: map[string]*Package{},
		loading:  map[string]bool{},
	}
	l.modRoot, l.modPath, err = findModule(absDir)
	if err != nil {
		return nil, err
	}
	// The runtime is part of every program: the compiler calls into it for
	// panics, memory copies and allocation, and it holds the entry point.
	// Loading it first puts it (and its imports) first in initialisation order.
	// pluto/alloc installs the default allocator in hosted builds.
	for _, p := range []string{"pluto/rt", "pluto/alloc"} {
		src, dir, err := l.resolve(p)
		if err != nil {
			return nil, err
		}
		if _, err := l.load(p, src, dir); err != nil {
			return nil, err
		}
	}

	mainPath := "main"
	if l.modRoot != "" {
		rel, err := filepath.Rel(l.modRoot, absDir)
		if err != nil {
			return nil, err
		}
		mainPath = path.Join(l.modPath, filepath.ToSlash(rel))
	}
	main, err := l.load(mainPath, osDir(absDir), absDir)
	if err != nil {
		return nil, err
	}
	if main.Types.Name() != "main" {
		return nil, fmt.Errorf("%s: package %s is not a main package", dir, main.Types.Name())
	}
	return &Program{Fset: l.fset, Main: main, Packages: l.order}, nil
}

type loader struct {
	cfg      *Config
	fset     *token.FileSet
	modRoot  string // directory holding pluto.mod, or ""
	modPath  string
	packages map[string]*Package
	loading  map[string]bool
	order    []*Package
}

// source abstracts where a package's files come from.
type source interface {
	list() ([]fs.DirEntry, error)
	read(name string) ([]byte, error)
	name(file string) string // file name for positions and messages
}

type osDir string

func (d osDir) list() ([]fs.DirEntry, error)     { return os.ReadDir(string(d)) }
func (d osDir) read(name string) ([]byte, error) { return os.ReadFile(filepath.Join(string(d), name)) }
func (d osDir) name(file string) string          { return filepath.Join(string(d), file) }

type fsDir struct {
	fsys fs.FS
	dir  string
}

func (d fsDir) list() ([]fs.DirEntry, error)     { return fs.ReadDir(d.fsys, d.dir) }
func (d fsDir) read(name string) ([]byte, error) { return fs.ReadFile(d.fsys, path.Join(d.dir, name)) }
func (d fsDir) name(file string) string          { return "$PLUTOROOT/" + path.Join(d.dir, file) }

// resolve maps an import path to its source.
func (l *loader) resolve(importPath string) (source, string, error) {
	if importPath == "pluto" || strings.HasPrefix(importPath, "pluto/") {
		if l.cfg.Stdlib == nil {
			return nil, "", fmt.Errorf("no standard library configured for %q", importPath)
		}
		if _, err := fs.Stat(l.cfg.Stdlib, importPath); err != nil {
			return nil, "", fmt.Errorf("standard library package %q not found", importPath)
		}
		return fsDir{l.cfg.Stdlib, importPath}, "$PLUTOROOT/" + importPath, nil
	}
	if l.modRoot != "" && (importPath == l.modPath || strings.HasPrefix(importPath, l.modPath+"/")) {
		rel := strings.TrimPrefix(strings.TrimPrefix(importPath, l.modPath), "/")
		dir := filepath.Join(l.modRoot, filepath.FromSlash(rel))
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return nil, "", fmt.Errorf("package %q not found (looked in %s)", importPath, dir)
		}
		return osDir(dir), dir, nil
	}
	if l.modRoot == "" {
		return nil, "", fmt.Errorf("cannot import %q: no pluto.mod declares a module, so only pluto/ packages can be imported", importPath)
	}
	return nil, "", fmt.Errorf("cannot import %q: it is outside module %s", importPath, l.modPath)
}

func (l *loader) load(importPath string, src source, dir string) (*Package, error) {
	if p := l.packages[importPath]; p != nil {
		return p, nil
	}
	if l.loading[importPath] {
		return nil, fmt.Errorf("import cycle through %q", importPath)
	}
	l.loading[importPath] = true
	defer delete(l.loading, importPath)

	pkg := &Package{Path: importPath, Dir: dir}
	if err := l.parse(pkg, src); err != nil {
		return nil, err
	}

	// Load imports first, in a stable order.
	var paths []string
	for _, f := range pkg.Files {
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, "\"`")
			if p != "unsafe" && !slices.Contains(paths, p) {
				paths = append(paths, p)
			}
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		isrc, idir, err := l.resolve(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", importPath, err)
		}
		dep, err := l.load(p, isrc, idir)
		if err != nil {
			return nil, err
		}
		pkg.Imports = append(pkg.Imports, dep)
	}

	pkg.Info = &types2.Info{
		Types:      map[ast.Expr]types2.TypeAndValue{},
		Instances:  map[*ast.Ident]types2.Instance{},
		Defs:       map[*ast.Ident]types2.Object{},
		Uses:       map[*ast.Ident]types2.Object{},
		Implicits:  map[ast.Node]types2.Object{},
		Selections: map[*ast.SelectorExpr]*types2.Selection{},
		Scopes:     map[ast.Node]*types2.Scope{},
	}
	var errs []error
	conf := types2.Config{
		Importer: importerFunc(func(p string) (*types2.Package, error) {
			if p == "unsafe" {
				return types2.Unsafe, nil
			}
			if dep := l.packages[p]; dep != nil {
				return dep.Types, nil
			}
			return nil, fmt.Errorf("package %q not loaded", p)
		}),
		Sizes: l.cfg.Sizes,
		Error: func(err error) {
			if len(errs) < 50 {
				errs = append(errs, err)
			}
		},
	}
	pkg.Types, _ = conf.Check(importPath, l.fset, pkg.Files, pkg.Info)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	l.packages[importPath] = pkg
	l.order = append(l.order, pkg)
	return pkg, nil
}

// parse reads the package's .p and .s files that match the build tags.
func (l *loader) parse(pkg *Package, src source) error {
	entries, err := src.list()
	if err != nil {
		return fmt.Errorf("reading package %q: %w", pkg.Path, err)
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, "_test.p") {
			continue
		}
		ext := path.Ext(name)
		isAsm := ext == ".s"
		if ext != ".s" && ext != ".p" && ext != ".c" && ext != ".o" {
			continue
		}
		if !l.nameMatches(name) {
			continue
		}
		data, err := src.read(name)
		if err != nil {
			return err
		}
		if ext == ".o" {
			pkg.ObjFiles = append(pkg.ObjFiles, AsmFile{Name: src.name(name), Src: data})
			continue
		}
		ok, err := l.constraintMatches(data, isAsm)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src.name(name), err))
			continue
		}
		if !ok {
			continue
		}
		if isAsm {
			pkg.AsmFiles = append(pkg.AsmFiles, AsmFile{Name: src.name(name), Src: data})
			continue
		}
		if ext == ".c" {
			pkg.CFiles = append(pkg.CFiles, AsmFile{Name: src.name(name), Src: data})
			continue
		}
		f, err := parser.ParseFile(l.fset, src.name(name), data, parser.ParseComments)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		pkg.Files = append(pkg.Files, f)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	if len(pkg.Files) == 0 {
		return fmt.Errorf("package %q: no Plutonium source files for this build", pkg.Path)
	}
	name := pkg.Files[0].Name.Name
	for _, f := range pkg.Files[1:] {
		if f.Name.Name != name {
			return fmt.Errorf("package %q: files declare both package %s and %s", pkg.Path, name, f.Name.Name)
		}
	}
	return nil
}

// knownSuffixTags are the tags that a file name suffix may name.
var knownSuffixTags = map[string]bool{
	"amd64": true, "arm64": true,
	"linux": true, "none": true,
}

// nameMatches applies file name suffix rules: name_tag.p and name_os_arch.p.
func (l *loader) nameMatches(name string) bool {
	base := strings.TrimSuffix(name, path.Ext(name))
	parts := strings.Split(base, "_")
	n := len(parts)
	for i := n - 1; i >= 1 && i >= n-2; i-- {
		if t := parts[i]; knownSuffixTags[t] && !l.cfg.Tags[t] {
			return false
		}
	}
	return true
}

// constraintMatches evaluates the //pu:build line (or #pu:build in assembly)
// before the package clause.
func (l *loader) constraintMatches(data []byte, isAsm bool) (bool, error) {
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var expr string
		switch {
		case strings.HasPrefix(line, "//pu:build "):
			expr = strings.TrimPrefix(line, "//pu:build ")
		case isAsm && strings.HasPrefix(line, "#pu:build "):
			expr = strings.TrimPrefix(line, "#pu:build ")
		case strings.HasPrefix(line, "//") || isAsm && strings.HasPrefix(line, "#"):
			continue // other comments before the package clause
		default:
			return true, nil // reached code without a constraint
		}
		c, err := constraint.Parse("//go:build " + expr)
		if err != nil {
			return false, fmt.Errorf("bad build constraint %q: %w", expr, err)
		}
		return c.Eval(func(tag string) bool { return l.cfg.Tags[tag] }), nil
	}
	return true, nil
}

// findModule walks up from dir looking for pluto.mod.
func findModule(dir string) (root, modPath string, err error) {
	for d := dir; ; {
		data, err := os.ReadFile(filepath.Join(d, "pluto.mod"))
		if err == nil {
			line, _, _ := strings.Cut(string(data), "\n")
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[0] != "module" {
				return "", "", fmt.Errorf("%s: first line must be \"module <path>\"", filepath.Join(d, "pluto.mod"))
			}
			if fields[1] == "pluto" || strings.HasPrefix(fields[1], "pluto/") {
				return "", "", fmt.Errorf("%s: module path %q is reserved for the standard library", filepath.Join(d, "pluto.mod"), fields[1])
			}
			return d, fields[1], nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", nil
		}
		d = parent
	}
}

type importerFunc func(path string) (*types2.Package, error)

func (f importerFunc) Import(path string) (*types2.Package, error) { return f(path) }
