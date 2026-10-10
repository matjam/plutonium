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

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/directive"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/scanner"
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
	// imports them (pluto/alloc/debug for --alloc=debug).
	Preload []string
}

// A Package is a loaded, type-checked package.
type Package struct {
	Path     string // import path
	Dir      string // directory, for messages
	Files    []*ast.File
	Tests    []*ast.File // the _test.p files among Files, for the package under test in LoadTest
	AsmFiles []AsmFile   // .s files selected by build tags
	CFiles   []AsmFile   // .c files selected by build tags, compiled with the system C compiler
	ObjFiles []AsmFile   // .o files selected by file name tags, linked as they are
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
	// Root is the directory that the program's own source files are
	// named relative to by TrimmedName: the module root (the directory
	// holding pluto.mod), or the main package's directory when there is no
	// module. It is "" when the program has no source files on disk other
	// than the standard library's (testing a standard library package).
	Root string
}

// StdPrefix starts the position file name of every standard library file:
// "$PLUTOROOT/pluto/fmt/print.p". Positions keep the prefix whether the
// library is embedded in the compiler or read from $PLUTOROOT.
const StdPrefix = "$PLUTOROOT/"

// TrimmedName returns the name that debugging information and stack traces
// record for the source file named name in the program's positions, with
// the build machine's directories removed, as go build -trimpath does:
//
//   - a file under Root is "./" followed by its slash-separated path
//     relative to Root ("./main.p", "./cmd/tool/main.p");
//   - a standard library file is its import path and file name
//     ("pluto/fmt/print.p"), the same whether the library is embedded or
//     read from $PLUTOROOT;
//   - any other name (a generated file such as _testmain.p) is unchanged.
//
// The result depends only on the module's contents, so the same source
// gives the same binary wherever it is built.
func (p *Program) TrimmedName(name string) string {
	if rest, ok := strings.CutPrefix(name, StdPrefix); ok {
		return rest
	}
	if p.Root == "" || !filepath.IsAbs(name) {
		return name
	}
	rel, err := filepath.Rel(p.Root, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return name
	}
	return "./" + filepath.ToSlash(rel)
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
		return nil, fail(diag.NotMainPackage, "%s: package %s is not a main package", dir, main.Types.Name())
	}
	root := l.modRoot
	if root == "" {
		root = absDir
	}
	return &Program{Fset: l.fset, Main: main, Packages: l.order, Root: root}, nil
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
		src              source
		dir, ipath, root string
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
			return nil, fail(diag.NotPackageDir, "%s: not a package directory", target)
		}
		l.modRoot, l.modPath, err = findModule(absDir)
		if err != nil {
			return nil, err
		}
		src, dir, ipath, root = osDir(absDir), absDir, l.dirPath(absDir), l.modRoot
		if l.modRoot == "" {
			root = absDir
			ipath = filepath.Base(absDir)
			if isStdPath(ipath) || ipath == "main" {
				return nil, fail(diag.TestDirName, "%s: a package outside a module cannot be tested from a directory named %q", target, ipath)
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
		return nil, fail(diag.TestMainPackage, "%s: testing a main package is not supported; move the code under test into a package of its own", target)
	}
	mainSrc, err := gen(l.fset, pkg)
	if err != nil {
		return nil, err
	}
	main, err := l.load("main", memDir{TestMainFile: mainSrc}, TestMainFile)
	if err != nil {
		return nil, err
	}
	return &Program{Fset: l.fset, Main: main, Packages: l.order, Root: root}, nil
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
func (d fsDir) name(file string) string          { return StdPrefix + path.Join(d.dir, file) }

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
			return nil, "", fail(diag.StdPackageMissing, "no standard library configured for %q", importPath)
		}
		if _, err := fs.Stat(l.cfg.Stdlib, importPath); err != nil {
			return nil, "", fail(diag.StdPackageMissing, "standard library package %q not found", importPath)
		}
		return fsDir{l.cfg.Stdlib, importPath}, StdPrefix + importPath, nil
	}
	if l.modRoot != "" && (importPath == l.modPath || strings.HasPrefix(importPath, l.modPath+"/")) {
		rel := strings.TrimPrefix(strings.TrimPrefix(importPath, l.modPath), "/")
		dir := filepath.Join(l.modRoot, filepath.FromSlash(rel))
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return nil, "", fail(diag.PackageNotFound, "package %q not found (looked in %s)", importPath, dir)
		}
		return osDir(dir), dir, nil
	}
	if l.modRoot == "" {
		return nil, "", fail(diag.ImportNoModule, "cannot import %q: no pluto.mod declares a module, so only pluto/ packages can be imported", importPath)
	}
	return nil, "", fail(diag.ImportOutside, "cannot import %q: it is outside module %s", importPath, l.modPath)
}

// fail returns a diagnostic without a position in the source.
func fail(code diag.Code, format string, args ...any) *diag.Diagnostic {
	return diag.New(token.Position{}, code, format, args...)
}

// prefixed returns err with prefix and ": " before its message, keeping
// its code if it is a diagnostic.
func prefixed(prefix string, err error) error {
	var d *diag.Diagnostic
	if errors.As(err, &d) {
		c := *d
		c.Msg = prefix + ": " + d.Msg
		return &c
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func (l *loader) load(importPath string, src source, dir string) (*Package, error) {
	if p := l.packages[importPath]; p != nil {
		return p, nil
	}
	if l.loading[importPath] {
		return nil, fail(diag.ImportCycle, "import cycle through %q", importPath)
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
			return nil, prefixed(importPath, err)
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
	var errs []error // types2.Error
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
	// A //pu: comment the compiler would ignore is an error too.
	var list diag.List
	for _, f := range pkg.Files {
		list = append(list, directive.Check(l.fset, f)...)
	}
	if len(errs) > 0 {
		ctx := &diag.CheckContext{Fset: l.fset, Files: pkg.Files, Pkg: pkg.Types, Info: pkg.Info}
		for _, err := range errs {
			list = append(list, diag.FromChecker(err, ctx))
		}
	}
	if len(list) > 0 {
		return nil, list
	}
	l.packages[importPath] = pkg
	l.order = append(l.order, pkg)
	return pkg, nil
}

// parse reads the package's .p and .s files that match the build tags.
func (l *loader) parse(pkg *Package, src source) error {
	entries, err := src.list()
	if err != nil {
		return fail(diag.ReadPackage, "reading package %q: %v", pkg.Path, err)
	}
	var errs diag.List
	for _, e := range entries {
		name := e.Name()
		isTest := strings.HasSuffix(name, "_test.p")
		if e.IsDir() || isTest && pkg.Path != l.testPath {
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
			errs = append(errs, fail(diag.BadConstraint, "%s: %v", src.name(name), err))
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
		if list, ok := err.(scanner.ErrorList); ok {
			for _, e := range list {
				errs = append(errs, diag.FromSyntax(e, data))
			}
			continue
		} else if err != nil {
			return err
		}
		pkg.Files = append(pkg.Files, f)
		if isTest {
			pkg.Tests = append(pkg.Tests, f)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	if len(pkg.Files) == 0 {
		return fail(diag.NoSourceFiles, "package %q: no Plutonium source files for this build", pkg.Path)
	}
	for _, f := range pkg.Tests {
		if n := f.Name.Name; strings.HasSuffix(n, "_test") {
			return fail(diag.ExternalTestPkg, "%s: package %s: external test packages are not supported; declare the test in package %s",
				l.fset.File(f.Pos()).Name(), n, strings.TrimSuffix(n, "_test"))
		}
	}
	name := pkg.Files[0].Name.Name
	for _, f := range pkg.Files[1:] {
		if f.Name.Name != name {
			return fail(diag.MixedPackages, "package %q: files declare both package %s and %s", pkg.Path, name, f.Name.Name)
		}
	}
	return nil
}

// knownSuffixTags are the tags that a file name suffix may name.
var knownSuffixTags = map[string]bool{
	"amd64": true, "arm64": true,
	"linux": true, "none": true,
}

// BuildTags returns the build tags that are set for a build mode on
// x86-64: hosted Linux, or freestanding (and kernel) with no operating
// system, plus the extra tags.
func BuildTags(freestanding, kernel bool, extra []string) map[string]bool {
	tags := map[string]bool{"amd64": true}
	if freestanding || kernel {
		tags["none"] = true
		tags["freestanding"] = true
		if kernel {
			tags["kernel"] = true
		}
	} else {
		tags["linux"] = true
		tags["hosted"] = true
	}
	for _, t := range extra {
		tags[t] = true
	}
	return tags
}

// MatchFile reports whether a Plutonium source file belongs in a build with
// the given tags: its name suffixes (name_tag.p, name_os_arch.p) and its
// //pu:build line must both allow it. Editors use it to choose a file's
// package the way the loader does.
func MatchFile(tags map[string]bool, name string, src []byte) (bool, error) {
	if !nameMatches(tags, name) {
		return false, nil
	}
	return constraintMatches(tags, src, path.Ext(name) == ".s")
}

// FindModule walks up from the absolute directory dir looking for
// pluto.mod, and returns the directory holding it and the module path, or
// two empty strings if there is none.
func FindModule(dir string) (root, modPath string, err error) { return findModule(dir) }

func (l *loader) nameMatches(name string) bool { return nameMatches(l.cfg.Tags, name) }

func (l *loader) constraintMatches(data []byte, isAsm bool) (bool, error) {
	return constraintMatches(l.cfg.Tags, data, isAsm)
}

// nameMatches applies file name suffix rules: name_tag.p and name_os_arch.p,
// and the same before _test.p.
func nameMatches(tags map[string]bool, name string) bool {
	base := strings.TrimSuffix(name, path.Ext(name))
	base = strings.TrimSuffix(base, "_test")
	parts := strings.Split(base, "_")
	n := len(parts)
	for i := n - 1; i >= 1 && i >= n-2; i-- {
		if t := parts[i]; knownSuffixTags[t] && !tags[t] {
			return false
		}
	}
	return true
}

// constraintMatches evaluates the //pu:build line (or #pu:build in assembly)
// before the package clause.
func constraintMatches(tags map[string]bool, data []byte, isAsm bool) (bool, error) {
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
		return c.Eval(func(tag string) bool { return tags[tag] }), nil
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
				return "", "", fail(diag.BadModFile, "%s: first line must be \"module <path>\"", filepath.Join(d, "pluto.mod"))
			}
			if fields[1] == "pluto" || strings.HasPrefix(fields[1], "pluto/") {
				return "", "", fail(diag.ReservedModule, "%s: module path %q is reserved for the standard library", filepath.Join(d, "pluto.mod"), fields[1])
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
