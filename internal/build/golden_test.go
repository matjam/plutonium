package build_test

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/testguard"
)

var update = flag.Bool("update", false, "regenerate testdata/run/*.out by running each program with Go")

// TestRun compiles and runs every program in testdata/run and compares its
// output with the .out file next to it. The programs are written in the
// common subset of Go and Plutonium, so -update produces the expected output
// by running them with the Go toolchain. A program may import pluto/fmt,
// which Go runs as its fmt.
func TestRun(t *testing.T) {
	testguard.Parallel(t)
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "run", "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".p")
		golden := strings.TrimSuffix(file, ".p") + ".out"
		if *update {
			out, err := runWithGo(file)
			if err != nil {
				t.Errorf("%s: go run: %v\n%s", name, err, out)
				continue
			}
			if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Errorf("%s: %v (run with -update to create it)", name, err)
			continue
		}
		for _, level := range []int{0, 2} {
			t.Run(name+"/O"+string(rune('0'+level)), func(t *testing.T) {
				testguard.Parallel(t)
				src, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				got, status := compileAndRun(t, string(src), level)
				if status != 0 {
					t.Errorf("exit status %d", status)
				}
				if got != string(want) {
					t.Errorf("output differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
				}
			})
		}
	}
}

// goPackages are the pluto/... packages that programs in testdata/run may
// import and that Go runs as its package of the same path without the
// pluto/ prefix.
var goPackages = []string{"cmp", "container/heap", "errors", "fmt", "slices", "strings", "unicode"}

// runWithGo runs a test program with the Go toolchain and returns what it
// writes to standard error (println's destination).
func runWithGo(file string) (string, error) {
	dir, err := os.MkdirTemp("", "goprog")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	src = translateTaskToGo(src)
	// pluto/fmt is Plutonium's fmt: Go runs the program with its own. The
	// other packages listed are the Plutonium ports of Go packages with
	// the same API, for the part a program in the common subset can use.
	for _, pkg := range goPackages {
		src = bytes.ReplaceAll(src, []byte(`"pluto/`+pkg+`"`), []byte(`"`+pkg+`"`))
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module prog\n\ngo 1.27\n"), 0o644); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// translateTaskToGo rewrites every task statement keyword in src to go, and
// every call of strings.Slice and strings.SliceFrom of pluto/strings to
// the slice expression it stands for (Plutonium has no slice expression on
// a string, but the programs only cut at rune boundaries, where Go's cut
// gives the same string), so that the program can be run by the Go
// toolchain when its .out file is regenerated. The nodes are found by
// parsing with the Plutonium parser, so the word task in an identifier or a
// comment is left alone. If the source does not parse, it is returned
// unchanged and the Go toolchain reports the error.
func translateTaskToGo(src []byte) []byte {
	for {
		out, changed := translateTaskToGoOnce(src)
		if !changed {
			return out
		}
		src = out
	}
}

// translateTaskToGoOnce makes the edits of translateTaskToGo that do not
// contain each other; a call of Slice inside the argument of another is
// left to the next round.
func translateTaskToGoOnce(src []byte) ([]byte, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		return src, false
	}
	// The import name of pluto/strings, if the program imports it.
	stringsName := ""
	for _, imp := range f.Imports {
		if imp.Path.Value == `"pluto/strings"` {
			stringsName = "strings"
			if imp.Name != nil {
				stringsName = imp.Name.Name
			}
		}
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	last := 0
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	text := func(e ast.Expr) string { return string(src[off(e.Pos()):off(e.End())]) }
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.TaskStmt:
			if !n.Go {
				edits = append(edits, edit{off(n.Task), off(n.Task) + len("task"), "go"})
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || stringsName == "" || off(n.Pos()) < last {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != stringsName {
				return true
			}
			var rewritten string
			switch {
			case sel.Sel.Name == "Slice" && len(n.Args) == 3:
				rewritten = "(" + text(n.Args[0]) + ")[" + text(n.Args[1]) + ":" + text(n.Args[2]) + "]"
			case sel.Sel.Name == "SliceFrom" && len(n.Args) == 2:
				rewritten = "(" + text(n.Args[0]) + ")[" + text(n.Args[1]) + ":]"
			default:
				return true
			}
			edits = append(edits, edit{off(n.Pos()), off(n.End()), rewritten})
			last = off(n.End())
			return false
		}
		return true
	})
	if len(edits) == 0 {
		return src, false
	}
	out := make([]byte, 0, len(src))
	at := 0
	for _, e := range edits {
		out = append(out, src[at:e.start]...)
		out = append(out, e.text...)
		at = e.end
	}
	out = append(out, src[at:]...)
	if use := "\nvar _ = " + stringsName + ".Compare\n"; stringsName != "" && !bytes.Contains(out, []byte(use)) {
		// The program may use pluto/strings for nothing but the calls just
		// replaced; Go rejects an import that is not used.
		out = append(out, use...)
	}
	return out, true
}
