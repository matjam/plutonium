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
	// Preload lists standard library packages to load, and so initialise,
	// right after pluto/rt and pluto/alloc, whether or not the program
	// imports them (pluto/alloc/debug for -alloc=debug).
	Preload []string
}

// A Package is a loaded, type-checked package.
type Package struct {
	Path     string // import path
	Dir      string // directory, for messages
	Files    []*ast.File
	Tests    []*ast.File // the _test.p files among Files, for the package under test in LoadTest
	AsmFiles []AsmFile   // .s files selected by build tags
	Types    *types2.Package
	Info     *types2.Info
	Imports  []*Package
}

// An AsmFile is an assembly source file belonging to a package.
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
	l := newLoader(cfg)
	l.modRoot, l.modPath, err = findModule(absDir)
	if err != nil {
		return nil, err
	}
	if err := l.loadRuntime(); err != nil {
		return nil, err
	}
	main, err := l.load(l.dirPath(absDir), osDir(absDir), absDir)
	if err != nil {
		return nil, err
	}
	if main.Types.Name() != "main" {
		return nil, fmt.Errorf("%s: package %s is not a main package", dir, main.Types.Name())
	}
	return &Program{Fset: l.fset, Main: main, Packages: l.order}, nil
}

// TestMainFile is the file name of the main package that LoadTest
// generates, as it appears in positions and messages.
const TestMainFile = "_testmain.p"

// LoadTest loads a package together with its _test.p files, then a main
// package for testing it: gen receives the loaded package and returns the
// source of that main package, which may import the package under test by
// its Path. The program's Main is the generated package.
//
// target is a directory, or the import path of a standard library package
// (one under pluto/). A package directory outside any module is imported
// under the name of its directory.
func LoadTest(cfg *Config, target string, gen func(*token.FileSet, *Package) ([]byte, error)) (*Program, error) {
	l := newLoader(cfg)
	var (
		src        source
		dir, ipath string
	)
	if isStdPath(target) {
		var err error
		src, dir, err = l.resolve(target)
		if err != nil {
			return nil, err
		}
		ipath = target
	} else {
		absDir, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		if st, err := os.Stat(absDir); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("%s: not a package directory", target)
		}
		l.modRoot, l.modPath, err = findModule(absDir)
		if err != nil {
			return nil, err
		}
		src, dir, ipath = osDir(absDir), absDir, l.dirPath(absDir)
		if l.modRoot == "" {
			ipath = filepath.Base(absDir)
			if isStdPath(ipath) || ipath == "main" {
				return nil, fmt.Errorf("%s: a package outside a module cannot be tested from a directory named %q", target, ipath)
			}
		}
	}
	l.testPath = ipath
	if err := l.loadRuntime(); err != nil {
		return nil, err
	}
	pkg, err := l.load(ipath, src, dir)
	if err != nil {
		return nil, err
	}
	if pkg.Types.Name() == "main" {
		return nil, fmt.Errorf("%s: testing a main package is not supported; move the code under test into a package of its own", target)
	}
	mainSrc, err := gen(l.fset, pkg)
	if err != nil {
		return nil, err
	}
	main, err := l.load("main", memDir{TestMainFile: mainSrc}, TestMainFile)
	if err != nil {
		return nil, err
	}
	return &Program{Fset: l.fset, Main: main, Packages: l.order}, nil
}

func isStdPath(p string) bool { return p == "pluto" || strings.HasPrefix(p, "pluto/") }

type loader struct {
	cfg      *Config
	fset     *token.FileSet
	modRoot  string // directory holding pluto.mod, or ""
	modPath  string
	testPath string // import path of the package whose _test.p files are included
	packages map[string]*Package
	loading  map[string]bool
	order    []*Package
}

func newLoader(cfg *Config) *loader {
	return &loader{
		cfg:      cfg,
		fset:     token.NewFileSet(),
		packages: map[string]*Package{},
		loading:  map[string]bool{},
	}
}

// loadRuntime loads the packages that are part of every program. The
// compiler calls into the runtime for panics, memory copies and
// allocation, and it holds the entry point. Loading it first puts it (and
// its imports) first in initialisation order. pluto/alloc installs the
// default allocator in hosted builds; Config.Preload packages come next.
func (l *loader) loadRuntime() error {
	for _, p := range append([]string{"pluto/rt", "pluto/alloc"}, l.cfg.Preload...) {
		src, dir, err := l.resolve(p)
		if err != nil {
			return err
		}
		if _, err := l.load(p, src, dir); err != nil {
			return err
		}
	}
	return nil
}

// dirPath returns the import path of the package in absDir: its path in
// the module, or "main" without one.
func (l *loader) dirPath(absDir string) string {
	if l.modRoot == "" {
		return "main"
	}
	rel, err := filepath.Rel(l.modRoot, absDir)
	if err != nil {
		return "main"
	}
	return path.Join(l.modPath, filepath.ToSlash(rel))
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

// memDir is a package held in memory: file name to contents.
type memDir map[string][]byte

func (d memDir) list() ([]fs.DirEntry, error) {
	var list []fs.DirEntry
	for name := range d {
		list = append(list, memEntry(name))
	}
	slices.SortFunc(list, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return list, nil
}

func (d memDir) read(name string) ([]byte, error) {
	if src, ok := d[name]; ok {
		return src, nil
	}
	return nil, fs.ErrNotExist
}

func (d memDir) name(file string) string { return file }

// memEntry is a regular file in a memDir.
type memEntry string

func (e memEntry) Name() string               { return string(e) }
func (e memEntry) IsDir() bool                { return false }
func (e memEntry) Type() fs.FileMode          { return 0 }
func (e memEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrInvalid }

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
		// A loaded package needs no resolving. This also lets a test main
		// import a package under test that lies outside any module.
		if dep := l.packages[p]; dep != nil {
			pkg.Imports = append(pkg.Imports, dep)
			continue
		}
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
		isTest := strings.HasSuffix(name, "_test.p")
		if e.IsDir() || isTest && pkg.Path != l.testPath {
			continue
		}
		isAsm := strings.HasSuffix(name, ".s")
		if !isAsm && !strings.HasSuffix(name, ".p") {
			continue
		}
		if !l.nameMatches(name) {
			continue
		}
		data, err := src.read(name)
		if err != nil {
			return err
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
		f, err := parser.ParseFile(l.fset, src.name(name), data, parser.ParseComments)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		pkg.Files = append(pkg.Files, f)
		if isTest {
			pkg.Tests = append(pkg.Tests, f)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	if len(pkg.Files) == 0 {
		return fmt.Errorf("package %q: no Plutonium source files for this build", pkg.Path)
	}
	for _, f := range pkg.Tests {
		if n := f.Name.Name; strings.HasSuffix(n, "_test") {
			return fmt.Errorf("%s: package %s: external test packages are not supported; declare the test in package %s",
				l.fset.File(f.Pos()).Name(), n, strings.TrimSuffix(n, "_test"))
		}
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

// nameMatches applies file name suffix rules: name_tag.p and name_os_arch.p,
// and the same before _test.p.
func (l *loader) nameMatches(name string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(name, ".p"), ".s")
	base = strings.TrimSuffix(base, "_test")
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
