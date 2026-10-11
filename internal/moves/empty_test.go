package moves_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/testguard"
	"github.com/matjam/plutonium/internal/types2"
)

// analyze loads src as a main package and returns what the move checker
// tells lowering, which must report no errors.
func analyze(t *testing.T, src string) (*moves.Info, *load.Program) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{
		Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	info, err := moves.Analyze(prog)
	if err != nil {
		t.Fatalf("move checker: %s", fullText(err))
	}
	return info, prog
}

// facts describes what info knows about the main package, one line each,
// sorted: "line kind: var parts" for exits, "line fresh: target" for
// assignments, "line results: var" for return statements and "line stmt:
// var parts" for statements a call may leave for Goexit.
func facts(info *moves.Info, prog *load.Program) string {
	fset := prog.Fset
	line := func(n ast.Node) int { return fset.Position(n.Pos()).Line }
	held := func(k moves.Known) string {
		var parts []string
		for v, h := range k {
			p := v.Name() + "="
			if h.Box {
				p += "box"
			}
			var ps []string
			for _, path := range h.Parts {
				ps = append(ps, strings.Join(path, ""))
			}
			sort.Strings(ps)
			p += "{" + strings.Join(ps, ",") + "}"
			parts = append(parts, p)
		}
		sort.Strings(parts)
		return strings.Join(parts, " ")
	}
	var out []string
	main := func(n ast.Node) bool {
		return strings.HasSuffix(fset.Position(n.Pos()).Filename, "main.p")
	}
	for n, k := range info.Empty {
		if main(n) {
			out = append(out, fmt.Sprintf("%d %T: %s", line(n), n, held(k)))
		}
	}
	for n, k := range info.AtStmt {
		if main(n) {
			out = append(out, fmt.Sprintf("%d stmt %T: %s", line(n), n, held(k)))
		}
	}
	for e := range info.Fresh {
		if main(e) {
			out = append(out, fmt.Sprintf("%d fresh: %s", line(e), types2.ExprString(e)))
		}
	}
	for st, vs := range info.EmptyResults {
		if main(st) {
			for v := range vs {
				out = append(out, fmt.Sprintf("%d results: %s", line(st), v.Name()))
			}
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

const emptyDecls = `package main

type C owner struct{ id int }

func (c *C) Drop()       {}
func (c *C) Set(id int)  { c.id = id }
func use(c C)            {}
func cond() bool         { return true }

type Pair owner struct{ x, y C }
type Node owner struct{ c C; n int; s string }
`

// TestEmptyFacts checks what the move checker tells lowering holds nothing
// (empty.go): at the exits of scopes, before returns replace named results,
// at the statements a call may leave for Goexit, and at assignments.
func TestEmptyFacts(t *testing.T) {
	testguard.Parallel(t)
	for _, tc := range []struct {
		name, src, want string
	}{
		{"moved and declared", `
func main() {
	a := C{id: 1}
	use(a)
	var b C
	_ = b.id
}`, `13 *ast.BlockStmt: a={} b={}
16 stmt *ast.DeclStmt: a={}
17 stmt *ast.AssignStmt: a={}`},
		{"assignments", `
func main() {
	var c C
	c = C{id: 1}
	var p Pair
	p.x = C{id: 2}
	p.y = C{id: 3}
	p.x = C{id: 4}
	var arr [3]C
	arr[1] = C{id: 5}
	arr[cond2()] = C{id: 6}
	_ = c.id
}
func cond2() int { return 0 }`, `13 *ast.BlockStmt: arr={[*],[1]} p={.0,.1}
15 fresh: c
17 fresh: p.x
18 fresh: p.y
20 stmt *ast.DeclStmt: p={.0,.1}
21 fresh: arr[1]
21 stmt *ast.AssignStmt: p={.0,.1}
22 stmt *ast.AssignStmt: p={.0,.1}
23 stmt *ast.AssignStmt: arr={[*],[1]} p={.0,.1}`},
		{"boxes, literals and take", `
func main() {
	n := new(Node)
	n.c = C{id: 1}
	_ = n.n
	q := Pair{y: C{id: 2}}
	c := take(&n.c)
	use(c)
	use(take(&q.y))
}`, `13 *ast.BlockStmt: c={} n=box{} q={}
15 fresh: n.c
17 stmt *ast.AssignStmt: n=box{.0}
18 stmt *ast.AssignStmt: q={.1}
19 stmt *ast.ExprStmt: n=box{} q={.1}
20 stmt *ast.ExprStmt: c={} n=box{}`},
		{"named results and returns", `
func named() (c C) {
	c = C{id: 1}
	return
}
func replaced() (c C) {
	return C{id: 2}
}
func early(b bool) C {
	x := C{id: 3}
	y := C{id: 4}
	if b {
		use(x)
		return y
	}
	use(y)
	return x
}
func main() {}`, `14 fresh: c
18 *ast.ReturnStmt: c={}
18 results: c
18 stmt *ast.ReturnStmt: c={}
25 *ast.ReturnStmt: x={} y={}
25 stmt *ast.ReturnStmt: x={}
28 *ast.ReturnStmt: x={} y={}
28 stmt *ast.ReturnStmt: y={}`},
		{"what is not tracked", `
func main() {
	a := C{id: 1}
	p := &a
	use(a)
	_ = p
	b := C{id: 2}
	f := func() { b.Set(3) }
	use(b)
	f()
	c := C{id: 4}
	defer c.Set(5)
	use(c)
	d := C{id: 6}
	defer d.Drop()
	if cond() && cond() {
		use(d)
	}
	e := C{id: 7}
	if cond() || f2(e) {
	}
}
func f2(c C) bool { return true }`, `13 *ast.BlockStmt: d={}
27 *ast.BlockStmt: d={}
27 *ast.IfStmt: d={}
30 stmt *ast.AssignStmt: d={}
31 *ast.BlockStmt: d={}
31 *ast.IfStmt: d={}
31 stmt *ast.IfStmt: d={}`},
		{"loops, switches and branches", `
func main() {
	for i := 0; i < 3; i++ {
		c := C{id: i}
		if i == 1 {
			use(c)
			continue
		}
		if i == 2 {
			break
		}
		use(c)
	}
	d := C{id: 9}
	switch {
	case cond():
		use(d)
	default:
	}
}`, `14 *ast.BlockStmt: c={}
14 stmt *ast.IncDecStmt: c={}
18 *ast.BranchStmt: c={}
18 stmt *ast.BranchStmt: c={}
27 *ast.CaseClause: d={}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testguard.Parallel(t)
			info, prog := analyze(t, emptyDecls+tc.src)
			if got := facts(info, prog); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
