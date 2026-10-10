package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Go's escape analysis is the oracle for where the translation must
// allocate. In Go, &T{...} and []T{...} live on the heap when the compiler
// sees them outlive their frame; in Plutonium they always live in the frame
// (language spec section 6), so the ones Go moves to the heap must allocate
// explicitly. Go reports its decisions with -gcflags=-m.

// escapes maps "file.go:line:col" to Go's escape analysis messages there.
type escapes map[string][]string

var escLine = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.*)$`)

// goEscapes runs Go's escape analysis on the package in dir, which is rel
// below the module root. The go command reports positions relative to the
// module root. Inlining is off (-l): with it, an allocation in an inlined
// function or closure is reported at each call site instead of where it is
// written. Without inlining the analysis is more conservative, which only
// adds allocations.
func goEscapes(dir, rel string) (escapes, error) {
	cmd := exec.Command("go", "build", "-gcflags=-m -l", "-o", os.DevNull, ".")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stderr = &out
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go build -gcflags=-m in %s: %v\n%s", dir, err, out.String())
	}
	esc := escapes{}
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		m := escLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		if d := filepath.Dir(m[1]); d != "." && d != rel {
			continue // a header line, or a position in another package
		}
		key := filepath.Base(m[1]) + ":" + m[2] + ":" + m[3]
		esc[key] = append(esc[key], m[4])
	}
	return esc, sc.Err()
}

// at returns the messages at p.
func (e escapes) at(fset *token.FileSet, p token.Pos) []string {
	pos := fset.Position(p)
	return e[fmt.Sprintf("%s:%d:%d", filepath.Base(pos.Filename), pos.Line, pos.Column)]
}

// has reports whether a message at p starts with prefix and ends with
// suffix.
func (e escapes) has(fset *token.FileSet, p token.Pos, prefix, suffix string) bool {
	for _, m := range e.at(fset, p) {
		if strings.HasPrefix(m, prefix) && strings.HasSuffix(m, suffix) {
			return true
		}
	}
	return false
}
