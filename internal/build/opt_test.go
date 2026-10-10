package build_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// listFunc compiles src at -O2 and returns the machine listing of function
// name (for example "main.f").
func listFunc(t *testing.T, src, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var listing bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib(), OptLevel: 2, Listing: &listing}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	text := listing.String()
	i := strings.Index(text, "\n"+name+":\n")
	if i < 0 {
		t.Fatalf("%s not in listing:\n%s", name, text)
	}
	text = text[i+1:]
	if j := strings.Index(text, "\n\n"); j >= 0 {
		text = text[:j]
	}
	return text
}

func TestBranchElim(t *testing.T) {
	const src = `package main

var data [64]int64

//pu:noinline
func step(n int64) int64 {
	steps := int64(0)
	for n != 1 {
		if n%2 == 0 {
			n = n / 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return steps
}

//pu:noinline
func child(root int64) int64 {
	c := 2*root + 1
	if data[c] < data[c+1] {
		c++
	}
	return data[c]
}

func main() { println(step(7), child(3)) }
`
	// The parity test becomes a cmov, leaving only the loop branch; 3n+1
	// is one LEA.
	asm := listFunc(t, src, "main.step")
	if !strings.Contains(asm, "cmov") || strings.Contains(asm, "imul") || strings.Count(asm, "\tj") > 2 {
		t.Errorf("step: want branch-free code with cmov and lea\n%s", asm)
	}
	// The selected child indexes memory, so it stays a branch.
	if asm := listFunc(t, src, "main.child"); strings.Contains(asm, "cmov") {
		t.Errorf("child: selected index feeds a load; want a branch\n%s", asm)
	}
}

func TestProveBoundsChecks(t *testing.T) {
	tests := []struct {
		name, body string
		checked    bool
	}{
		{"len", `for i := 0; i < len(s); i++ { t += s[i] }`, false},
		{"globalStep3", `for i := 0; i < 100; i += 3 { t += g[i] }`, false},
		// i += 2 could step past MaxInt when nothing bounds len(s).
		{"lenStep2", `for i := 0; i < len(s); i += 2 { t += s[i] }`, true},
		{"global", `for i := 0; i < 100; i++ { t += g[i] }`, false},
		{"leq", `for i := 0; i <= len(s); i++ { t += s[i] }`, true},
		{"negStart", `for i := -1; i < len(s); i++ { t += s[i] }`, true},
		{"globalPast", `for i := 0; i < 101; i++ { t += g[i] }`, true},
		{"unbounded", `for i := 0; i < n; i++ { t += s[i] }`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := "package main\n\nvar g [100]int64\n\n//pu:noinline\nfunc f(s []int64, n int) int64 {\n\tt := int64(0)\n\t" +
				tc.body + "\n\treturn t\n}\n\nfunc main() { println(f(nil, 0)) }\n"
			asm := listFunc(t, src, "main.f")
			if got := strings.Contains(asm, "panicIndex"); got != tc.checked {
				t.Errorf("bounds check present = %v, want %v\n%s", got, tc.checked, asm)
			}
		})
	}
}
