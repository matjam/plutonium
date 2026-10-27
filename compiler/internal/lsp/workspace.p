// Translated by go2pu from internal/lsp/workspace.go; edited by hand since.

package lsp

import (
	"compiler/compat"
	"compiler/compat/fs"
	"compiler/internal/diag"
	"compiler/internal/directive"
	"compiler/internal/load"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/parser"
	"compiler/internal/syntax/scanner"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/slices"
	"pluto/strconv"
	"pluto/strings"
)

// A workspace type-checks packages from disk and from the editor's open
// documents, and caches the results until a file they depend on changes.
// Unlike the compiler's loader it keeps going after errors, so that a
// package with mistakes still has types for hover and completion.
//
// A package is checked once per build mode, since build tags choose its
// files. The standard library is checked like any other package and stays
// cached across edits, since edits do not touch it.
type workspace struct {
	fset    *token.FileSet
	stdRoot string            // directory holding pluto/, the configured standard library
	overlay map[string][]byte // open documents by path
	modes   []buildMode       // the configured mode first
	pkgs    map[pkgKey]*pkg
	files   map[*token.File]*srcFile // every file of a cached package
	roots   []string                 // workspace folders
}

// A buildMode is a set of build tags a package can be checked under.
type buildMode struct {
	name string
	tags map[string]bool
}

// modesFor returns the build modes for settings: the configured one first,
// then the others, so that a file excluded from the configured mode can
// still be checked in the mode it is written for.
func modesFor(s Settings) ([]buildMode, error) {
	target := s.Target
	if target == "" {
		target = "x86_64-linux"
	}
	var def string
	switch target {
	case "x86_64-linux":
		def = "hosted"
	case "x86_64-none":
		def = "freestanding"
	default:
		return nil, compat.NewError(compat.Sprintfb("unsupported target %q (supported: x86_64-linux, x86_64-none)", compat.ArgS(target)))
	}
	mode := s.Mode
	switch mode {
	case "":
		mode = def
	case "hosted":
		if def != "hosted" {
			return nil, compat.NewError("hosted mode needs an operating system target")
		}
	case "freestanding", "kernel":
	default:
		return nil, compat.NewError(compat.Sprintfb("unknown mode %q (supported: hosted, freestanding, kernel)", compat.ArgS(mode)))
	}
	modes := compat.Clone([]buildMode{newMode(mode, s.Tags)})
	for _, m := range []string{"hosted", "freestanding", "kernel"} {
		if m != mode {
			modes = compat.Append(modes, newMode(m, s.Tags))
		}
	}
	return modes, nil
}

func newMode(name string, extra []string) buildMode {
	return buildMode{name: name, tags: load.BuildTags(name != "hosted", name == "kernel", extra)}
}

func newWorkspace(stdRoot string, modes []buildMode) *workspace {
	return pu.New(workspace{
		fset:    token.NewFileSet(),
		stdRoot: stdRoot,
		overlay: map[string][]byte{},
		modes:   modes,
		pkgs:    map[pkgKey]*pkg{},
		files:   map[*token.File]*srcFile{},
	})
}

type pkgKey struct {
	dir   string
	mode  string
	tests bool // the package's _test.p files are included
}

// A pkg is a checked package. Its types and info are complete as far as
// the errors allow.
type pkg struct {
	key     pkgKey
	path    string // import path
	files   []*srcFile
	types   *types2.Package
	info    *types2.Info
	errors  []checkError
	deps    []*pkg
	modRoot string // directory holding the package's pluto.mod, or ""
}

// A srcFile is a parsed file of a package.
type srcFile struct {
	path string
	*text
	ast *ast.File
	tok *token.File
	pkg *pkg
}

// A checkError is a parse or type error, at a byte range of a file, with
// its diagnostic from internal/diag (code and explanation).
type checkError struct {
	path       string
	start, end int
	diag       *diag.Diagnostic
}

// newCheckError returns the error d at offset off of the file path with
// contents src.
func newCheckError(path string, src []byte, off int, d *diag.Diagnostic) checkError {
	off = min(max(off, 0), len(src))
	return checkError{path: path, start: off, end: wordEnd(src, off), diag: d}
}

// read returns a file's contents: the open document if there is one,
// otherwise the file on disk.
func (w *workspace) read(path string) ([]byte, error) {
	if src, ok := w.overlay[path]; ok {
		return src, nil
	}
	return compat.ReadFile(path)
}

// listDir returns the names of the Plutonium source files in dir, on disk
// or open in the editor.
func (w *workspace) listDir(dir string) []string {
	var names []string
	entries, _ := fs.OSReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".p") {
			names = compat.Append(names, e.Name())
		}
	}
	for p := range w.overlay {
		if filepath.Dir(p) == dir && strings.HasSuffix(p, ".p") {
			names = compat.Append(names, filepath.Base(p))
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// modeFor returns the build mode to check the file at path under: the
// first mode whose tags select it. ok is false if none does.
func (w *workspace) modeFor(path string) (mode buildMode, ok bool) {
	src, err := w.read(path)
	if err != nil {
		return w.modes[0], false
	}
	for _, m := range w.modes {
		if match, err := load.MatchFile(m.tags, filepath.Base(path), src); match || err != nil {
			return m, err == nil
		}
	}
	return w.modes[0], false
}

// fileOf returns the checked file at path and its package, checking the
// package if needed. The package includes its tests. f is nil if the
// build mode leaves the file out of its package (or the file has no
// package at all).
func (w *workspace) fileOf(path string) (*srcFile, *pkg, error) {
	mode, _ := w.modeFor(path)
	p := w.check(filepath.Dir(path), mode, true, map[pkgKey]bool{}) or return
	for _, f := range p.files {
		if f.path == path {
			return f, p, nil
		}
	}
	return nil, p, nil
}

// srcFileAt returns the file holding pos, which may belong to any cached
// package.
func (w *workspace) srcFileAt(pos token.Pos) *srcFile {
	if !pos.IsValid() {
		return nil
	}
	tf := w.fset.File(pos)
	if tf == nil {
		return nil
	}
	return w.files[tf]
}

// stdRootFor returns the standard library a package in dir is checked
// against. A package inside a standard library tree (dir/../pluto/rt
// exists) uses that tree, so editing the library's own sources checks
// them against each other; anything else uses the configured library.
func (w *workspace) stdRootFor(dir string) string {
	for d := dir; ; {
		if filepath.Base(d) == "pluto" {
			if st, err := fs.OSStat(filepath.Join(d, "rt")); err == nil && st.IsDir() {
				return filepath.Dir(d)
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return w.stdRoot
		}
		d = parent
	}
}

// check returns the package in dir under mode, with its _test.p files if
// tests is set, checking it and its imports unless they are cached.
func (w *workspace) check(dir string, mode buildMode, tests bool, loading map[pkgKey]bool) (*pkg, error) {
	names := w.listDir(dir)
	if tests && !slices.ContainsFunc(names, func(n string) bool { return strings.HasSuffix(n, "_test.p") }) {
		tests = false
	}
	key := pkgKey{dir: dir, mode: mode.name, tests: tests}
	if p := w.pkgs[key]; p != nil {
		return p, nil
	}
	if loading[key] {
		return nil, compat.NewError("import cycle")
	}
	loading[key] = true
	defer delete(loading, key)

	p := pu.New(pkg{key: key})
	for _, name := range names {
		if !tests && strings.HasSuffix(name, "_test.p") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := w.read(path)
		if err != nil {
			continue
		}
		match, err := load.MatchFile(mode.tags, name, src)
		if err != nil {
			p.errors = compat.Append(p.errors, newCheckError(path, src, 0, diag.New(token.Position{}, diag.BadConstraint, "%s", pu.Box(err.Error()))))
			continue
		}
		if !match {
			continue
		}
		f, err := parser.ParseFile(w.fset, path, pu.Box(src), parser.ParseComments|parser.AllErrors|parser.SkipObjectResolution)
		if list, ok := err.(*scanner.ErrorList); ok {
			for _, e := range *list {
				p.errors = compat.Append(p.errors, newCheckError(path, src, e.Pos.Offset, diag.FromSyntax(e, src)))
			}
		}
		if f == nil {
			continue
		}
		p.files = compat.Append(p.files, pu.New(srcFile{path: path, text: newText(src), ast: f, tok: w.fset.File(f.Pos()), pkg: p}))
	}
	if len(p.files) == 0 {
		return nil, compat.NewError(compat.Sprintfb("no Plutonium source files in %s for %s mode", compat.ArgS(dir), compat.ArgS(mode.name)))
	}

	stdRoot := w.stdRootFor(dir)
	stdDir := filepath.Join(stdRoot, "pluto")
	modRoot, modPath, _ := load.FindModule(dir)
	p.modRoot = modRoot
	pkgName := p.files[0].ast.Name.Name
	switch {
	case hasPathPrefix(dir, stdDir):
		rel, _ := filepath.Rel(stdRoot, dir)
		p.path = filepath.ToSlash(rel)
		p.modRoot = ""
	case modRoot != "":
		rel, _ := filepath.Rel(modRoot, dir)
		p.path = filepath.Join(modPath, filepath.ToSlash(rel)) // path.Join: the same on Linux
	default:
		p.path = pkgName
	}

	// Check the imports, then the package.
	im := pu.New(importer{imported: map[string]*types2.Package{}, importErr: map[string]error{}})
	imported, importErr := im.imported, im.importErr
	for _, f := range p.files {
		for _, spec := range f.ast.Imports {
			ipath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || ipath == "unsafe" || imported[ipath] != nil || importErr[ipath] != nil {
				continue
			}
			depDir, err := w.resolve(ipath, stdRoot, modRoot, modPath)
			if err == nil {
				var dep *pkg
				if dep, err = w.check(depDir, mode, false, loading); err == nil {
					imported[ipath] = dep.types
					p.deps = compat.Append(p.deps, dep)
				}
			}
			if err != nil {
				importErr[ipath] = err
			}
		}
	}
	p.info = pu.New(types2.Info{
		Types:      map[ast.Expr]types2.TypeAndValue{},
		Instances:  map[*ast.Ident]types2.Instance{},
		Defs:       map[*ast.Ident]types2.Object{},
		Uses:       map[*ast.Ident]types2.Object{},
		Implicits:  map[ast.Node]types2.Object{},
		Selections: map[*ast.SelectorExpr]*types2.Selection{},
		Scopes:     map[ast.Node]*types2.Scope{},
	})
	typeErrs := pu.Alloc[[]error]() // types2.Error
	byTok := map[*token.File]*srcFile{}
	for _, f := range p.files {
		byTok[f.tok] = f
	}
	conf := pu.New(types2.Config{
		Importer: im,
		Sizes:    sizes,
		Errors:   typeErrs,
	})
	files := compat.Make[*ast.File](len(p.files))
	for i, f := range p.files {
		files[i] = f.ast
	}
	// The Go version recovers from a crash of the checker on a broken
	// syntax tree; Plutonium has no recover, so such a crash ends the
	// server.
	p.types, _ = conf.Check(p.path, w.fset, files, p.info)
	// Explain the type errors now that the checker has recorded everything.
	ctx := pu.New(diag.CheckContext{Fset: w.fset, Files: files, Pkg: p.types, Info: p.info})
	for _, err := range *typeErrs {
		terr, ok := err.(types2.Error)
		if !ok {
			continue
		}
		if f := byTok[w.fset.File(terr.Pos)]; f != nil {
			p.errors = compat.Append(p.errors, newCheckError(f.path, f.src, f.tok.Offset(terr.Pos), diag.FromChecker(err, ctx)))
		}
	}
	for _, f := range p.files {
		for _, d := range directive.Check(w.fset, f.ast) {
			p.errors = compat.Append(p.errors, newCheckError(f.path, f.src, d.Pos.Offset, d))
		}
	}
	w.pkgs[key] = p
	for _, f := range p.files {
		w.files[f.tok] = f
	}
	return p, nil
}

var sizes = types2.SizesFor("gc", "amd64")

// An importer gives the checker the packages a package imports, checked
// already, or the errors of checking them.
type importer struct {
	imported  map[string]*types2.Package
	importErr map[string]error
}

func (im *importer) Import(path string) (*types2.Package, error) {
	if path == "unsafe" {
		return types2.Unsafe, nil
	}
	if t := im.imported[path]; t != nil {
		return t, nil
	}
	if err := im.importErr[path]; err != nil {
		return nil, err
	}
	return nil, compat.NewError(compat.Sprintfb("package %q not found", compat.ArgS(path)))
}

// resolve returns the directory of an import path.
func (w *workspace) resolve(importPath, stdRoot, modRoot, modPath string) (string, error) {
	if importPath == "pluto" || strings.HasPrefix(importPath, "pluto/") {
		dir := filepath.Join(stdRoot, filepath.FromSlash(importPath))
		if st, err := fs.OSStat(dir); err != nil || !st.IsDir() {
			return "", compat.NewError(compat.Sprintfb("standard library package %q not found", compat.ArgS(importPath)))
		}
		return dir, nil
	}
	if modRoot == "" {
		return "", compat.NewError(compat.Sprintfb("cannot import %q: no pluto.mod declares a module, so only pluto/ packages can be imported", compat.ArgS(importPath)))
	}
	if importPath != modPath && !strings.HasPrefix(importPath, compat.Concat(modPath, "/")) {
		return "", compat.NewError(compat.Sprintfb("cannot import %q: it is outside module %s", compat.ArgS(importPath), compat.ArgS(modPath)))
	}
	dir := filepath.Join(modRoot, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(importPath, modPath), "/")))
	if st, err := fs.OSStat(dir); err != nil || !st.IsDir() {
		return "", compat.NewError(compat.Sprintfb("package %q not found (looked in %s)", compat.ArgS(importPath), compat.ArgS(dir)))
	}
	return dir, nil
}

// invalidate drops the cached packages in dir, in every mode, and every
// package that imports one of them.
func (w *workspace) invalidate(dir string) {
	gone := map[*pkg]bool{}
	for k, p := range w.pkgs {
		if k.dir == dir {
			gone[p] = true
		}
	}
	for changed := len(gone) > 0; changed; {
		changed = false
		for _, p := range w.pkgs {
			if !gone[p] && slices.ContainsFunc(p.deps, func(d *pkg) bool { return gone[d] }) {
				gone[p] = true
				changed = true
			}
		}
	}
	for p := range gone {
		delete(w.pkgs, p.key)
		w.removeFiles(p)
	}
}

// invalidateAll drops every cached package.
func (w *workspace) invalidateAll() {
	for _, p := range w.pkgs {
		w.removeFiles(p)
	}
	clear(w.pkgs)
}

func (w *workspace) removeFiles(p *pkg) {
	for _, f := range p.files {
		delete(w.files, f.tok)
		w.fset.RemoveFile(f.tok)
	}
}

// skipDir reports whether a directory walk of the workspace skips name.
func skipDir(name string) bool {
	return name == "testdata" || name == "node_modules" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// maxWorkspaceDirs bounds the package directories a workspace-wide
// operation looks at.
const maxWorkspaceDirs = 2000

// packageDirs returns the directories under the workspace folders that
// hold Plutonium source files.
func (w *workspace) packageDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	for _, root := range w.roots {
		_ = filepath.WalkDir(root, func(p string, d *os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			p = compat.Concat(p) // a copy: p is a view the walk reuses
			if d.IsDir() {
				if p != root && skipDir(d.NameLossy()) || len(dirs) >= maxWorkspaceDirs {
					return filepath.SkipDir
				}
				return nil
			}
			if dir := filepath.Dir(p); strings.HasSuffix(p, ".p") && !seen[dir] {
				seen[dir] = true
				dirs = compat.Append(dirs, dir)
			}
			return nil
		})
	}
	for p := range w.overlay {
		if dir := filepath.Dir(p); !seen[dir] && w.inWorkspace(dir) {
			seen[dir] = true
			dirs = compat.Append(dirs, dir)
		}
	}
	slices.Sort(dirs)
	return dirs
}

// inWorkspace reports whether path lies in a workspace folder.
func (w *workspace) inWorkspace(path string) bool {
	return slices.ContainsFunc(w.roots, func(r string) bool { return hasPathPrefix(path, r) })
}

// workspacePackages checks every package in the workspace, each in the
// mode its first file selects, and returns them.
func (w *workspace) workspacePackages() []*pkg {
	var pkgs []*pkg
	for _, dir := range w.packageDirs() {
		names := w.listDir(dir)
		if len(names) == 0 {
			continue
		}
		mode, _ := w.modeFor(filepath.Join(dir, names[0]))
		if p, err := w.check(dir, mode, true, map[pkgKey]bool{}); err == nil {
			pkgs = compat.Append(pkgs, p)
		}
	}
	return pkgs
}
