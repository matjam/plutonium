package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOracle runs the oracle on a small program and checks that the calls
// written in it, static and dynamic, are found applied, and that a call the
// analysis does not see is reported as missing.
func TestOracle(t *testing.T) {
	const src = `package main

import "pluto/fmt"

type I interface{ M(p *int) }

type T struct{ n int }

func (t *T) M(p *int) { t.n = *p }

func apply(f func(int) int, x int) int { return f(x) }

func double(x int) int { return 2 * x }

var ti T

type S struct{ n int }

func (s *S) FmtList() (any, int) { return nil, s.n }

type Outer struct {
	a int
	s S
}

func main() {
	var i I = &ti
	x := 3
	i.M(&x)
	println(apply(double, 2))
	fmt.Println(Outer{a: 1})
}
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := check(program{name: "t", dir: dir})
	if r.Calls == 0 || r.Targets == 0 {
		t.Fatalf("nothing compared: %+v", r)
	}
	for _, f := range r.Findings {
		if f.Kind == Missing && strings.HasPrefix(f.Callee, "main.") {
			t.Errorf("a call of the program's own code is missing: %s", f)
		}
		if f.Kind == Missing && strings.Contains(f.Callee, "FmtList") {
			t.Errorf("fmt's call of a pointer method through its walk is missing: %s", f)
		}
	}
	if s := (Finding{Kind: Missing, Callee: "f", How: "static", Why: "w"}).String(); !strings.Contains(s, "missing") || !strings.Contains(s, ": w") {
		t.Errorf("Finding.String() = %q", s)
	}
	// The D47 measurement: (*T).M is reached through I and keeps nothing of
	// p; (*S).FmtList is reached through fmt's walk and keeps nothing.
	ks, cands, err := keepersOf(program{name: "t", dir: dir})
	if err != nil || cands == 0 {
		t.Fatalf("keepersOf: %d candidates, %v", cands, err)
	}
	for _, k := range ks {
		if strings.HasPrefix(k.Func, "(*T).M") && k.Param == "p" {
			t.Errorf("(*T).M is said to keep p: %+v", k)
		}
	}
	if programsIn(filepath.Dir(dir)) == nil {
		t.Error("programsIn found nothing")
	}
}
