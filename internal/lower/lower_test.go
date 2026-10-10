package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/ssa"
)

// lowerSource writes src as a main package, loads it with the repository's
// standard library and lowers it.
func lowerSource(t *testing.T, src string) *ssa.Program {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs(filepath.Join("..", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{
		Tags:   map[string]bool{"linux": true, "amd64": true, "hosted": true},
		Stdlib: os.DirFS(lib),
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	out, err := lower.Lower(&lower.Config{}, prog)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	for _, f := range out.Funcs {
		if err := f.Verify(); err != nil {
			t.Error(err)
		}
	}
	return out
}

func TestLowerProgram(t *testing.T) {
	prog := lowerSource(t, `package main

type Point struct{ X, Y int }

func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }
func (p *Point) Scale(k int)      { p.X *= k; p.Y *= k }

type Shape interface{ Area() int }

type Rect struct{ w, h int }

func (r Rect) Area() int { return r.w * r.h }

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func sum(xs ...int) (total int) {
	for _, x := range xs {
		total += x
	}
	return
}

func Max[T ~int | ~float64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

var counter = 10

func main() int64 {
	p := Point{1, 2}.Add(Point{3, 4})
	p.Scale(2)
	var s Shape = Rect{3, 4}
	n := fib(10) + sum(1, 2, 3) + s.Area() + Max(3, 4) + counter
	arr := [4]int{1, 2, 3, 4}
	for i := range arr {
		n += arr[i]
	}
	str := "héllo"
	for _, r := range str {
		n += int(r)
	}
	f := func(k int) int { return k + n }
	n = f(1)
	defer println("done", n)
	switch {
	case n > 100:
		n = 100
	}
	return int64(n + p.X)
}
`)
	var names []string
	for _, f := range prog.Funcs {
		names = append(names, f.Name)
	}
	for _, want := range []string{"main.main", "main.fib", "main.sum", "main.Max[int]", "main.Point.Add", "main.(*Point).Scale", "pluto.main", "main.Rect.Area·i"} {
		found := false
		for _, n := range names {
			if strings.Contains(n, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("function %s not lowered; have %v", want, names)
		}
	}
}
