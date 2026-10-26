// Translated by go2pu from internal/load/package.go at the go-cutover tag; edited by hand since.

package load

import (
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"selfhost/compat"
	"selfhost/pu"
)

// LoadPackage loads an ordinary package and its dependencies without requiring
// an entry point or generating a test main. Tooling can inspect library source
// without executing initializers or linking its foreign declarations.
func LoadPackage(cfg *Config, target string) (*Program, error) {
	l := newLoader(cfg)
	var src source
	var dir, ipath, root string
	if isStdPath(target) {
		var err error
		src, dir, err = l.resolve(target)
		if err != nil {
			return nil, err
		}
		ipath = target
	} else {
		// An import path within the current module names the same source as
		// its directory. Explicit directories continue to work without a
		// module, including when the working directory is outside one.
		if !filepath.IsAbs(target) && !strings.HasPrefix(target, ".") {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			modRoot, modPath, err := findModule(cwd)
			if err != nil {
				return nil, err
			}
			if modPath != "" && (target == modPath || strings.HasPrefix(target, compat.Concat(modPath, "/"))) {
				target = filepath.Join(modRoot, filepath.FromSlash(strings.TrimPrefix(target, modPath)))
			}
		}
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		l.modRoot, l.modPath, err = findModule(abs)
		if err != nil {
			return nil, err
		}
		src, dir, ipath, root = pu.Box(osDir(abs)).(source), abs, l.dirPath(abs), l.modRoot
		if root == "" {
			root = abs
			ipath = "main"
		}
	}
	pkg := l.load(ipath, src, dir) or return
	return pu.New(Program{Fset: l.fset, Main: pkg, Packages: l.initOrder(), Root: root}), nil
}
