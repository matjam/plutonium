package load

import (
	"path/filepath"
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
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		l.modRoot, l.modPath, err = findModule(abs)
		if err != nil {
			return nil, err
		}
		src, dir, ipath, root = osDir(abs), abs, l.dirPath(abs), l.modRoot
		if root == "" {
			root = abs
			ipath = "main"
		}
	}
	pkg, err := l.load(ipath, src, dir)
	if err != nil {
		return nil, err
	}
	return &Program{Fset: l.fset, Main: pkg, Packages: l.initOrder(), Root: root}, nil
}
