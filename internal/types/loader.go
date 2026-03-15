package types

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/matjam/plutonium/internal/ast"
	"github.com/matjam/plutonium/internal/buildtag"
	"github.com/matjam/plutonium/internal/parser"
	"github.com/matjam/plutonium/internal/token"
)

// LoadConfig controls how packages are found and loaded.
type LoadConfig struct {
	// StdlibRoot is the path to the pluto standard library root directory.
	// e.g. "/home/user/src/Plutonium/pluto"
	StdlibRoot string

	// Tags are the active build tags for file filtering.
	Tags map[string]bool

	// Fset is the shared FileSet for all parsed files.
	Fset *token.FileSet
}

// packageCache stores already-loaded packages to avoid recompilation.
var packageCache = make(map[string]*Package)

// loadingSet tracks packages currently being loaded to detect circular imports.
var loadingSet = make(map[string]bool)

// LoadPackage loads, parses, and type-checks an imported package.
// importPath is like "pluto/os" or "pluto/syscall".
// Returns the type-checked Package with its exported scope.
func LoadPackage(importPath string, cfg *LoadConfig) (*Package, error) {
	// Check cache first
	if pkg, ok := packageCache[importPath]; ok {
		return pkg, nil
	}

	// Detect circular imports
	if loadingSet[importPath] {
		return nil, fmt.Errorf("circular import: %q", importPath)
	}
	loadingSet[importPath] = true
	defer delete(loadingSet, importPath)

	// Map import path to directory
	dir, err := resolveImportDir(importPath, cfg)
	if err != nil {
		return nil, fmt.Errorf("resolving %q: %w", importPath, err)
	}

	// Find .pu files in the directory, filtered by build tags
	sources, err := findSourceFiles(dir, cfg.Tags)
	if err != nil {
		return nil, fmt.Errorf("finding sources for %q: %w", importPath, err)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no source files found for package %q in %s", importPath, dir)
	}

	// Parse all source files
	var files []*ast.File
	for _, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", src, err)
		}
		f, err := parser.ParseFile(cfg.Fset, src, data, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", src, err)
		}
		files = append(files, f)
	}

	// Extract package name from first file
	pkgName := ""
	if len(files) > 0 && files[0].Name != nil {
		pkgName = files[0].Name.Name
	}

	// Type-check the package (this recursively loads its own imports)
	pkg, err := Check(pkgName, cfg.Fset, files, nil, cfg)
	if err != nil {
		return nil, fmt.Errorf("type-checking %q: %w", importPath, err)
	}

	// Cache it
	packageCache[importPath] = pkg
	return pkg, nil
}

// resolveImportDir maps an import path to a filesystem directory.
func resolveImportDir(importPath string, cfg *LoadConfig) (string, error) {
	// "pluto/os" → <StdlibRoot>/os
	// "pluto/syscall" → <StdlibRoot>/syscall
	if strings.HasPrefix(importPath, "pluto/") {
		subpath := strings.TrimPrefix(importPath, "pluto/")
		dir := filepath.Join(cfg.StdlibRoot, subpath)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("package directory not found: %s", dir)
		}
		return dir, nil
	}

	// "unsafe" is handled specially — it has no source files
	if importPath == "unsafe" {
		return "", fmt.Errorf("unsafe is a builtin pseudo-package")
	}

	return "", fmt.Errorf("unknown import path: %q", importPath)
}

// findSourceFiles finds all .pu files in dir that match the active build tags.
func findSourceFiles(dir string, tags map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".pu") {
			continue
		}
		path := filepath.Join(dir, name)

		// Read file to extract //pu:build directives
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		directives := extractBuildDirectives(string(data))

		ok, err := buildtag.FileMatches(name, directives, tags)
		if err != nil {
			return nil, err
		}
		if ok {
			sources = append(sources, path)
		}
	}
	return sources, nil
}

// extractBuildDirectives extracts //pu: directive texts from source.
func extractBuildDirectives(src string) []string {
	var dirs []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//pu:") {
			dirs = append(dirs, strings.TrimPrefix(line, "//"))
		}
	}
	return dirs
}

// ResetPackageCache clears the package cache. Useful for tests.
func ResetPackageCache() {
	packageCache = make(map[string]*Package)
	loadingSet = make(map[string]bool)
}
