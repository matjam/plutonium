package directive

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// The diagnostics of the directives, and the audit against the
// specification, are checked through pluto in tests (directive_test.go).

// TestTree checks every Plutonium source file of the repository, so that a
// retired or misspelt directive left in the library, an example or a
// benchmark fails here. The compiler's corpus under tests/, which holds the
// programs that exist to show these errors, is left out.
func TestTree(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	n := 0
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if e.Name() == ".git" || e.Name() == ".claude" || path == filepath.Join(root, "tests") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".p" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return nil // programs that test syntax errors
		}
		n++
		for _, d := range Check(fset, f) {
			t.Errorf("%v", d)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 100 {
		t.Errorf("checked only %d files", n)
	}
}
