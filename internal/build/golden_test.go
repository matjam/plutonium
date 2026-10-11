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
var goPackages = []string{"cmp", "container/heap", "fmt", "slices", "unicode"}

// runWithGo runs a test program with the Go toolchain and returns what it
// writes to standard error (println's destination).
func runWithGo(file string) (string, error) {
	dir, err := os.MkdirTemp("", "goprog")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
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

// translateTaskToGo rewrites every task statement keyword in src to go, so
// that the program can be run by the Go toolchain when its .out file is
// regenerated. The keywords are found by parsing with the Plutonium parser,
// so the word task in an identifier or a comment is left alone. If the source
// does not parse, it is returned unchanged and the Go toolchain reports the
// error.
func translateTaskToGo(src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		return src
	}
	// The offset of every task keyword, so that the walk below can replace
	// each one in place.
	starts := make(map[int]bool, 8)
	ast.Inspect(f, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TaskStmt); ok && !ts.Go {
			starts[fset.Position(ts.Task).Offset] = true
		}
		return true
	})
	if len(starts) == 0 {
		return src
	}
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		if starts[i] {
			out = append(out, "go"...)
			i += len("task")
			continue
		}
		j := i
		for j < len(src) && !starts[j] {
			j++
		}
		out = append(out, src[i:j]...)
		i = j
	}
	return out
}
