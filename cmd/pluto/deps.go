package main

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

// importedPackage describes a dependency package discovered during
// transitive import resolution.
type importedPackage struct {
	ImportPath string
	Dir        string
	Files      []string // .pu source file paths
}

// collectDependencies walks the AST of parsed files and collects all
// transitive import paths. Returns them in dependency order (leaves first).
func collectDependencies(files []*ast.File, stdlibRoot string, tags map[string]bool, fset *token.FileSet) ([]importedPackage, error) {
	seen := make(map[string]bool)
	var result []importedPackage

	for _, f := range files {
		for _, imp := range f.Imports {
			path := stripQuotes(imp.Path.Value)
			if path == "unsafe" {
				continue
			}
			if err := collectDepsRecursive(path, stdlibRoot, tags, fset, seen, &result); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// collectDepsRecursive resolves a single import path, parses its source files
// to discover its own imports, recurses into them, and finally appends the
// package to result (topological / leaves-first order).
func collectDepsRecursive(importPath, stdlibRoot string, tags map[string]bool, fset *token.FileSet, seen map[string]bool, result *[]importedPackage) error {
	if seen[importPath] {
		return nil
	}
	seen[importPath] = true

	// Resolve directory
	dir, err := resolveImportDirDeps(importPath, stdlibRoot)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", importPath, err)
	}

	// Find source files
	sources, err := findSourceFilesDeps(dir, tags)
	if err != nil {
		return fmt.Errorf("finding sources for %q: %w", importPath, err)
	}

	// Parse files to find their imports (for recursion)
	for _, src := range sources {
		data, readErr := os.ReadFile(src)
		if readErr != nil {
			continue
		}
		f, parseErr := parser.ParseFile(fset, src, data, 0)
		if parseErr != nil {
			continue
		}
		for _, imp := range f.Imports {
			depPath := stripQuotes(imp.Path.Value)
			if depPath == "unsafe" {
				continue
			}
			if err := collectDepsRecursive(depPath, stdlibRoot, tags, fset, seen, result); err != nil {
				return err
			}
		}
	}

	// Add this package AFTER its dependencies (topological order)
	*result = append(*result, importedPackage{
		ImportPath: importPath,
		Dir:        dir,
		Files:      sources,
	})
	return nil
}

// resolveImportDirDeps maps an import path to a filesystem directory.
// Used by the dependency collector (separate from the types package's resolver).
func resolveImportDirDeps(importPath, stdlibRoot string) (string, error) {
	if strings.HasPrefix(importPath, "pluto/") {
		subpath := strings.TrimPrefix(importPath, "pluto/")
		dir := filepath.Join(stdlibRoot, subpath)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
		return "", fmt.Errorf("package directory not found: %s", filepath.Join(stdlibRoot, subpath))
	}
	return "", fmt.Errorf("unknown import path: %q", importPath)
}

// findSourceFilesDeps finds all .pu files in dir that match the active build
// tags. Used by the dependency collector.
func findSourceFilesDeps(dir string, tags map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".p") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		directives := extractDirectives(string(data))
		ok, tagErr := buildtag.FileMatches(e.Name(), directives, tags)
		if tagErr != nil {
			continue
		}
		if ok {
			sources = append(sources, path)
		}
	}
	return sources, nil
}

// stripQuotes removes surrounding double quotes from a string literal.
func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
