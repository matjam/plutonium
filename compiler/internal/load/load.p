// Translated by go2pu from internal/load/load.go at the go-cutover tag; edited by hand since.

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
	"compiler/compat"
	"compiler/compat/fs"
	"compiler/internal/diag"
	"compiler/internal/directive"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/constraint"
	"compiler/internal/syntax/parser"
	"compiler/internal/syntax/scanner"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/errors"
	"pluto/path/filepath"
	path "pluto/path/filepath"
	"pluto/slices"
	"pluto/sort"
	"pluto/strings"
	"pluto/time"
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
	Fset *token.FileSet
	Main *Package
	// Packages is every package in initialization order (language spec
	// section 15.3), which puts every package after its imports.
	Packages []*Package
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
	return compat.Concat("./", filepath.ToSlash(rel))
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
	l.loadRuntime() or return
	main, err := l.load(l.dirPath(absDir), pu.Box(osDir(absDir)).(source), absDir)
	if err != nil {
		return nil, err
	}
	if main.Types.Name() != "main" {
		return nil, fail(diag.NotMainPackage, "%s: package %s is not a main package", pu.Box(dir), pu.Box(main.Types.Name()))
	}
	root := l.modRoot
	if root == "" {
		root = absDir
	}
	return pu.New(Program{Fset: l.fset, Main: main, Packages: l.initOrder(), Root: root}), nil
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
		if st, err := fs.OSStat(absDir); err != nil || !st.IsDir() {
			return nil, fail(diag.NotPackageDir, "%s: not a package directory", pu.Box(target))
		}
		l.modRoot, l.modPath, err = findModule(absDir)
		if err != nil {
			return nil, err
		}
		src, dir, ipath, root = pu.Box(osDir(absDir)).(source), absDir, l.dirPath(absDir), l.modRoot
		if l.modRoot == "" {
			root = absDir
			ipath = filepath.Base(absDir)
			if isStdPath(ipath) || ipath == "main" {
				return nil, fail(diag.TestDirName, "%s: a package outside a module cannot be tested from a directory named %q", pu.Box(target), pu.Box(ipath))
			}
		}
	}
	l.testPath = ipath
	l.loadRuntime() or return
	pkg, err := l.load(ipath, src, dir)
	if err != nil {
		return nil, err
	}
	if pkg.Types.Name() == "main" {
		return nil, fail(diag.TestMainPackage, "%s: testing a main package is not supported; move the code under test into a package of its own", pu.Box(target))
	}
	mainSrc, err := gen(l.fset, pkg)
	if err != nil {
		return nil, err
	}
	main, err := l.load("main", memDir{TestMainFile: mainSrc}, TestMainFile)
	if err != nil {
		return nil, err
	}
	return pu.New(Program{Fset: l.fset, Main: main, Packages: l.initOrder(), Root: root}), nil
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
	return pu.New(loader{
		cfg:      cfg,
		fset:     token.NewFileSet(),
		packages: map[string]*Package{},
		loading:  map[string]bool{},
	})
}

// loadRuntime loads the packages that are part of every program. The
// compiler calls into the runtime for panics, memory copies and
// allocation, and it holds the entry point. pluto/alloc installs the
// default allocator in hosted builds; Config.Preload packages replace it
// (pluto/alloc/debug). initOrder initializes these packages, and what they
// import, before any other.
func (l *loader) loadRuntime() error {
	for _, p := range l.runtimePackages() {
		src, dir := l.resolve(p) or return
		l.load(p, src, dir) or return
	}
	return nil
}

// runtimePackages returns the import paths of the packages that
// loadRuntime loads.
func (l *loader) runtimePackages() []string {
	return compat.Append(compat.Clone([]string{"pluto/rt", "pluto/alloc"}), l.cfg.Preload...)
}

// initOrder returns the loaded packages in the order the program
// initializes them (language spec section 15.3): first pluto/rt and
// pluto/alloc (and the Preload packages, which install the default
// allocator in their place) with the packages they import, then the
// remaining packages. Within each group, given the packages sorted by
// import path, each step initializes the first package in the list whose
// imports are all initialized.
func (l *loader) initOrder() []*Package {
	runtime := map[*Package]bool{}
	var mark func(p *Package)
	mark = func(p *Package) {
		if runtime[p] {
			return
		}
		runtime[p] = true
		for _, dep := range p.Imports {
			mark(dep)
		}
	}
	for _, ip := range l.runtimePackages() {
		if p := l.packages[ip]; p != nil {
			mark(p)
		}
	}
	var first, rest []*Package
	for _, p := range l.order {
		if runtime[p] {
			first = compat.Append(first, p)
		} else {
			rest = compat.Append(rest, p)
		}
	}
	done := map[*Package]bool{}
	var order []*Package
	for _, group := range [][]*Package{first, rest} {
		slices.SortFunc(group, func(a, b *Package) int { return strings.Compare(a.Path, b.Path) })
		for range group {
			for _, p := range group {
				if !done[p] && !slices.ContainsFunc(p.Imports, func(dep *Package) bool { return !done[dep] }) {
					done[p] = true
					order = compat.Append(order, p)
					break
				}
			}
		}
	}
	return order
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

func (d osDir) list() ([]fs.DirEntry, error) { return fs.OSReadDir(string(d)) }
func (d osDir) read(name string) ([]byte, error) {
	return compat.ReadFile(filepath.Join(string(d), name))
}
func (d osDir) name(file string) string { return filepath.Join(string(d), file) }

type fsDir struct {
	fsys fs.FS
	dir  string
}

func (d fsDir) list() ([]fs.DirEntry, error)     { return fs.ReadDir(d.fsys, d.dir) }
func (d fsDir) read(name string) ([]byte, error) { return fs.ReadFile(d.fsys, path.Join(d.dir, name)) }
func (d fsDir) name(file string) string          { return compat.Concat(StdPrefix, path.Join(d.dir, file)) }

// memDir is a package held in memory: file name to contents.
type memDir map[string][]byte

func (d memDir) list() ([]fs.DirEntry, error) {
	var list []fs.DirEntry
	for name := range d {
		list = compat.Append[fs.DirEntry](list, pu.Box(memEntry(name)).(fs.DirEntry))
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
	if code, msg, bad := types2.ForbiddenImport(importPath); bad {
		// Whatever the module says, reflect is E2005 (spec section 14.3).
		return nil, "", fail(diag.CheckerCode(code), "%s", pu.Box(msg))
	}
	if importPath == "pluto" || strings.HasPrefix(importPath, "pluto/") {
		if l.cfg.Stdlib == nil {
			return nil, "", fail(diag.StdPackageMissing, "no standard library configured for %q", pu.Box(importPath))
		}
		if _, err := fs.Stat(l.cfg.Stdlib, importPath); err != nil {
			return nil, "", fail(diag.StdPackageMissing, "standard library package %q not found", pu.Box(importPath))
		}
		return pu.New(fsDir{l.cfg.Stdlib, importPath}), compat.Concat(StdPrefix, importPath), nil
	}
	if l.modRoot != "" && (importPath == l.modPath || strings.HasPrefix(importPath, compat.Concat(l.modPath, "/"))) {
		rel := strings.TrimPrefix(strings.TrimPrefix(importPath, l.modPath), "/")
		dir := filepath.Join(l.modRoot, filepath.FromSlash(rel))
		if st, err := fs.OSStat(dir); err != nil || !st.IsDir() {
			return nil, "", fail(diag.PackageNotFound, "package %q not found (looked in %s)", pu.Box(importPath), pu.Box(dir))
		}
		return pu.New(osDir(dir)), dir, nil
	}
	if l.modRoot == "" {
		return nil, "", fail(diag.ImportNoModule, "cannot import %q: no pluto.mod declares a module, so only pluto/ packages can be imported", pu.Box(importPath))
	}
	return nil, "", fail(diag.ImportOutside, "cannot import %q: it is outside module %s", pu.Box(importPath), pu.Box(l.modPath))
}

// fail returns a diagnostic without a position in the source.
func fail(code diag.Code, format string, args ...any) *diag.Diagnostic {
	return diag.New(token.Position{}, code, format, args...)
}

// prefixed returns err with prefix and ": " before its message, keeping
// its code if it is a diagnostic.
func prefixed(prefix string, err error) error {
	d := pu.Alloc[*diag.Diagnostic]()
	if errors.As(err, d) {
		c := pu.New(*(*d))
		c.Msg = compat.Concat(prefix, ": ", (*d).Msg)
		return c
	}
	return compat.Wrap(compat.Sprintfb("%s: %w", compat.ArgS(prefix), compat.ArgW(err)), err)
}

// Timing, set by the driver for PLUTO_PHASES, makes the loader add up the
// time it spends parsing in ParseTime and type-checking in CheckTime.
var Timing bool

// ParseTime and CheckTime are the time spent parsing and type-checking,
// when Timing is set.
var ParseTime, CheckTime time.Duration

func (l *loader) load(importPath string, src source, dir string) (*Package, error) {
	if p := l.packages[importPath]; p != nil {
		return p, nil
	}
	if l.loading[importPath] {
		return nil, fail(diag.ImportCycle, "import cycle through %q", pu.Box(importPath))
	}
	l.loading[importPath] = true
	defer delete(l.loading, importPath)

	pkg := pu.New(Package{Path: importPath, Dir: dir})
	var start time.Time
	if Timing {
		start = time.Now()
	}
	l.parse(pkg, src) or return
	if Timing {
		ParseTime += time.Since(start)
	}

	// Load imports first, in a stable order.
	var paths []string
	for _, f := range pkg.Files {
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, "\"`")
			if p != "unsafe" && !slices.Contains(paths, p) {
				paths = compat.Append(paths, p)
			}
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		// A loaded package needs no resolving. This also lets a test main
		// import a package under test that lies outside any module.
		if dep := l.packages[p]; dep != nil {
			pkg.Imports = compat.Append(pkg.Imports, dep)
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
		pkg.Imports = compat.Append(pkg.Imports, dep)
	}

	pkg.Info = pu.New(types2.Info{
		Types:      map[ast.Expr]types2.TypeAndValue{},
		Instances:  map[*ast.Ident]types2.Instance{},
		Defs:       map[*ast.Ident]types2.Object{},
		Uses:       map[*ast.Ident]types2.Object{},
		Implicits:  map[ast.Node]types2.Object{},
		Selections: map[*ast.SelectorExpr]*types2.Selection{},
		Scopes:     map[ast.Node]*types2.Scope{},
	})
	errs := pu.Alloc[[]error]() // types2.Error
	conf := pu.New(types2.Config{
		Importer: pu.New(sourceImporter{l}),
		Sizes:    l.cfg.Sizes,
		Errors:   errs,
		// The first 50 errors.
		ErrorLimit: 50,
	})
	if Timing {
		start = time.Now()
	}
	pkg.Types, _ = conf.Check(importPath, l.fset, pkg.Files, pkg.Info)
	if Timing {
		CheckTime += time.Since(start)
	}
	// A //pu: comment the compiler would ignore is an error too.
	list := pu.Alloc[diag.List]()
	for _, f := range pkg.Files {
		(*list) = diag.List(compat.Append((*list), directive.Check(l.fset, f)...))
	}
	if len((*errs)) > 0 {
		ctx := pu.New(diag.CheckContext{Fset: l.fset, Files: pkg.Files, Pkg: pkg.Types, Info: pkg.Info})
		for _, err := range *errs {
			(*list) = diag.List(compat.Append((*list), diag.FromChecker(err, ctx)))
		}
	}
	if len((*list)) > 0 {
		return nil, list
	}
	l.packages[importPath] = pkg
	l.order = compat.Append(l.order, pkg)
	return pkg, nil
}

// parse reads the package's .p and .s files that match the build tags.
func (l *loader) parse(pkg *Package, src source) error {
	entries, err := src.list()
	if err != nil {
		return fail(diag.ReadPackage, "reading package %q: %v", pu.Box(pkg.Path), err)
	}
	errs := pu.Alloc[diag.List]()
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
			pkg.ObjFiles = compat.Append(pkg.ObjFiles, AsmFile{Name: src.name(name), Src: data})
			continue
		}
		ok, err := l.constraintMatches(data, isAsm)
		if err != nil {
			(*errs) = diag.List(compat.Append((*errs), fail(diag.BadConstraint, "%s: %v", pu.Box(src.name(name)), err)))
			continue
		}
		if !ok {
			continue
		}
		if isAsm {
			pkg.AsmFiles = compat.Append(pkg.AsmFiles, AsmFile{Name: src.name(name), Src: data})
			continue
		}
		if ext == ".c" {
			pkg.CFiles = compat.Append(pkg.CFiles, AsmFile{Name: src.name(name), Src: data})
			continue
		}
		f, err := parser.ParseFile(l.fset, src.name(name), pu.Box(data), parser.ParseComments)
		if list, ok := err.(*scanner.ErrorList); ok {
			for _, e := range *list {
				(*errs) = diag.List(compat.Append((*errs), diag.FromSyntax(e, data)))
			}
			continue
		} else if err != nil {
			return err
		}
		pkg.Files = compat.Append(pkg.Files, f)
		if isTest {
			pkg.Tests = compat.Append(pkg.Tests, f)
		}
	}
	if len((*errs)) > 0 {
		return errs
	}
	if len(pkg.Files) == 0 {
		return fail(diag.NoSourceFiles, "package %q: no Plutonium source files for this build", pu.Box(pkg.Path))
	}
	for _, f := range pkg.Tests {
		if n := f.Name.Name; strings.HasSuffix(n, "_test") {
			return fail(diag.ExternalTestPkg, "%s: package %s: external test packages are not supported; declare the test in package %s",
				pu.Box(l.fset.File(f.Pos()).Name()), pu.Box(n), pu.Box(strings.TrimSuffix(n, "_test")))
		}
	}
	name := pkg.Files[0].Name.Name
	for _, f := range pkg.Files[1:] {
		if f.Name.Name != name {
			return fail(diag.MixedPackages, "package %q: files declare both package %s and %s", pu.Box(pkg.Path), pu.Box(name), pu.Box(f.Name.Name))
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
	parts := compat.Split(base, "_")
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
	for line := range strings.Lines(compat.String(data)) {
		line = strings.TrimSpace(line)
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
		c, err := constraint.Parse(compat.Concat("//go:build ", expr))
		if err != nil {
			return false, compat.Wrap(compat.Sprintfb("bad build constraint %q: %w", compat.ArgS(expr), compat.ArgW(err)), err)
		}
		return constraint.EvalTags(c, tags), nil
	}
	return true, nil
}

// findModule walks up from dir looking for pluto.mod.
func findModule(dir string) (root, modPath string, err error) {
	for d := dir; ; {
		data, err := compat.ReadFile(filepath.Join(d, "pluto.mod"))
		if err == nil {
			line, _, _ := strings.Cut(compat.String(data), "\n")
			fields := compat.Fields(line)
			if len(fields) != 2 || fields[0] != "module" {
				return "", "", fail(diag.BadModFile, "%s: first line must be \"module <path>\"", pu.Box(filepath.Join(d, "pluto.mod")))
			}
			if fields[1] == "pluto" || strings.HasPrefix(fields[1], "pluto/") {
				return "", "", fail(diag.ReservedModule, "%s: module path %q is reserved for the standard library", pu.Box(filepath.Join(d, "pluto.mod")), pu.Box(fields[1]))
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

// A sourceImporter imports the packages the loader has loaded.
type sourceImporter struct{ l *loader }

func (im *sourceImporter) Import(p string) (*types2.Package, error) {
	if p == "unsafe" {
		return types2.Unsafe, nil
	}
	if dep := im.l.packages[p]; dep != nil {
		return dep.Types, nil
	}
	return nil, compat.NewError(compat.Sprintfb("package %q not loaded", compat.ArgS(p)))
}
