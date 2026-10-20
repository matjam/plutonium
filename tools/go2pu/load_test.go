package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// TestTranslationsLoad loads every checked-in translation (the packages
// under selfhost/internal, selfhost/compat and the others) with the
// compiler's loader, which parses and type-checks them as Plutonium. This is
// the front end's verdict on the translations; building them also needs the
// lifetime and race analyses, which TestDifferential reaches through the
// demos.
func TestTranslationsLoad(t *testing.T) {
	root := t.TempDir()
	var pkgs []string
	err := filepath.WalkDir(selfhostDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(selfhostDir, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".p") && !strings.HasSuffix(path, "_test.p") &&
			!strings.HasPrefix(rel, "demo"+string(filepath.Separator)) {
			pkgs = append(pkgs, "selfhost/"+filepath.ToSlash(filepath.Dir(rel)))
		}
		return os.WriteFile(filepath.Join(root, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(pkgs)
	var imports []string
	for i, p := range pkgs {
		if i == 0 || p != pkgs[i-1] {
			imports = append(imports, "\t_ \""+p+"\"")
		}
	}
	main := "package main\n\nimport (\n" + strings.Join(imports, "\n") + "\n)\n\nfunc main() {}\n"
	if err := os.MkdirAll(filepath.Join(root, "loadcheck"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loadcheck", "main.p"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	prog, err := load.Load(cfg, filepath.Join(root, "loadcheck"))
	if err != nil {
		t.Fatalf("the translations do not load:\n%v", err)
	}
	loaded := map[string]bool{}
	for _, p := range prog.Packages {
		loaded[p.Path] = true
	}
	for _, p := range pkgs {
		if !loaded[p] {
			t.Errorf("package %s was not loaded", p)
		}
	}
}
