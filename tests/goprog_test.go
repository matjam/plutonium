package tests

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// goPackages are the pluto/... packages that programs in testdata/run may
// import and that Go runs as its package of the same path without the
// pluto/ prefix.
var goPackages = []string{"cmp", "container/heap", "errors", "fmt", "slices", "strings", "unicode"}

// runWithGo runs a program of testdata/run with the Go toolchain and
// returns its combined output (println writes to standard error).
func runWithGo(t *testing.T, file string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	src = taskToGo(src)
	// pluto/fmt is Plutonium's fmt: Go runs the program with its own. The
	// other packages listed are the Plutonium ports of Go packages with
	// the same API, for the part a program in the common subset can use.
	for _, pkg := range goPackages {
		src = bytes.ReplaceAll(src, []byte(`"pluto/`+pkg+`"`), []byte(`"`+pkg+`"`))
	}
	src = sliceToGo(src)
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

// taskToGo rewrites the keyword of every task statement in src to go. Go's
// scanner reads task as an identifier; it is the keyword where it starts a
// statement and the call it starts follows, so a variable named task (which
// an operator, a selector or a comma follows) and the word in a comment or
// a string are left alone.
func taskToGo(src []byte) []byte {
	fset := token.NewFileSet()
	file := fset.AddFile("main.p", -1, len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, 0)
	type tok struct {
		pos token.Pos
		tok token.Token
		lit string
	}
	var toks []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		toks = append(toks, tok{pos, t, lit})
	}
	out := bytes.Clone(src)
	for i, k := range toks {
		if k.tok != token.IDENT || k.lit != "task" || i+1 == len(toks) {
			continue
		}
		if i > 0 {
			switch toks[i-1].tok {
			case token.SEMICOLON, token.LBRACE, token.COLON:
			default:
				continue
			}
		}
		switch toks[i+1].tok {
		case token.IDENT, token.FUNC, token.LPAREN:
		default:
			continue
		}
		off := file.Offset(k.pos)
		// "go" is shorter than "task": pad with spaces to keep offsets.
		copy(out[off:], "go  ")
	}
	return out
}

// sliceToGo rewrites every call of strings.Slice and strings.SliceFrom (the
// strings package being Go's now, where pluto/strings was imported) to the
// slice expression it stands for. Plutonium has no slice expression on a
// string, but the programs only cut at rune boundaries, where Go's cut gives
// the same string. If the source does not parse, it is returned unchanged
// and the Go toolchain reports the error.
func sliceToGo(src []byte) []byte {
	for {
		out, changed := sliceToGoOnce(src)
		if !changed {
			return out
		}
		src = out
	}
}

// sliceToGoOnce makes the edits of sliceToGo that do not contain each
// other; a call of Slice inside the argument of another is left to the next
// round.
func sliceToGoOnce(src []byte) ([]byte, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.SkipObjectResolution)
	if err != nil {
		return src, false
	}
	stringsName := ""
	for _, imp := range f.Imports {
		if imp.Path.Value == `"strings"` {
			stringsName = "strings"
			if imp.Name != nil {
				stringsName = imp.Name.Name
			}
		}
	}
	if stringsName == "" {
		return src, false
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
		call, ok := n.(*ast.CallExpr)
		if !ok || off(call.Pos()) < last {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != stringsName {
			return true
		}
		var rewritten string
		switch {
		case sel.Sel.Name == "Slice" && len(call.Args) == 3:
			rewritten = "(" + text(call.Args[0]) + ")[" + text(call.Args[1]) + ":" + text(call.Args[2]) + "]"
		case sel.Sel.Name == "SliceFrom" && len(call.Args) == 2:
			rewritten = "(" + text(call.Args[0]) + ")[" + text(call.Args[1]) + ":]"
		default:
			return true
		}
		edits = append(edits, edit{off(call.Pos()), off(call.End()), rewritten})
		last = off(call.End())
		return false
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
	if use := "\nvar _ = " + stringsName + ".Compare\n"; !bytes.Contains(out, []byte(use)) {
		// The program may use strings for nothing but the calls just
		// replaced; Go rejects an import that is not used.
		out = append(out, use...)
	}
	return out, true
}
