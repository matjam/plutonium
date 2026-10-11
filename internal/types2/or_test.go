package types2_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/testguard"
	"github.com/matjam/plutonium/internal/types2"
)

// orDecls declares the functions the or statement tests call.
const orDecls = `
type T struct{ name string }

type myErr error

type E struct{}

func (E) Error() string { return "" }

func load(string) (T, error)    { return T{}, nil }
func two() (int, string, error) { return 0, "", nil }
func fail() error               { return nil }
func one() int                  { return 0 }
func mine() (int, myErr)        { return 0, nil }
func impl() (int, E)            { return 0, E{} }
func none()                     {}
func atoi(string) (int, error)  { return 0, nil }
func use(...any)                {}
func exit()                     {}
func gen[V any](v V) (V, error) { return v, nil }
func wrap(error) error          { return nil }
func pair() (int, error)        { return 0, nil }
`

// orErrors type-checks src, with orDecls added, and returns its errors as
// "line: code" strings, in order.
func orErrors(t *testing.T, src string) ([]string, *types2.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.p", "package p\n"+src+orDecls, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var got []string
	info := &types2.Info{
		Types:     make(map[ast.Expr]types2.TypeAndValue),
		Defs:      make(map[*ast.Ident]types2.Object),
		Uses:      make(map[*ast.Ident]types2.Object),
		Implicits: make(map[ast.Node]types2.Object),
		Scopes:    make(map[ast.Node]*types2.Scope),
	}
	conf := types2.Config{Importer: unsafeOnly{}, Error: func(err error) {
		e := err.(types2.Error)
		got = append(got, fmt.Sprintf("%d: E%04d %s", fset.Position(e.Pos).Line, int(e.Code()), e.Msg))
	}}
	_, _ = conf.Check("p", fset, []*ast.File{f}, info)
	return got, info
}

// TestOrValid checks or statements that the type checker accepts
// (language spec 11.27).
func TestOrValid(t *testing.T) {
	testguard.Parallel(t)
	src := `
func a(path string) (int, error) {
	load(path) or return
	cfg := load(path) or return wrap(err)
	cfg = load(path) or return 0, err
	n, s := two() or return
	n, s = two() or panic(err)
	fail() or return
	m := gen(3) or return
	m = gen[int](4) or {
		use(err)
		return 0, err
	}
	use(cfg, n, s, m)
	for _, line := range []string{"1"} {
		k := atoi(line) or continue
		k = atoi(line) or break
		use(k)
		switch k := atoi(line) or continue; {
		case k > 0:
			atoi(line) or break // breaks the switch
		}
	}
	if v := atoi(path) or return; v > 0 {
		use(v)
	} else {
		use(v)
	}
	atoi(path) or {
		if n > 0 {
			return 0, err
		} else {
			panic(err)
		}
	}
	atoi(path) or {
		err := wrap(err) // a block clause may declare its own err
		return 0, err
	}
	atoi(path) or {
		err = wrap(err) // err is an ordinary variable
		return 0, err
	}
	atoi(path) or goto done
done:
	pair() or return pair() // an ordinary return of both results
	return 0, nil
}

func named(fields []string) (parsed int, err error) {
	for _, f := range fields {
		atoi(f) or return // err hides the named result: set by position
		parsed++
	}
	return
}

func errOnly() error {
	fail() or return
	fail() or return wrap(err)
	return nil
}

func oneResult() int {
	atoi("") or return 1 // an ordinary return
	return 0
}

func loop() {
Outer:
	for {
		for {
			atoi("") or break Outer
			atoi("") or continue Outer
		}
	}
}

func lit() {
	f := func() (int, error) {
		atoi("") or return
		return 0, nil
	}
	use(f)
}

func or(x int) int { return x } // or as a name

func names() {
	or := 3
	use(or)
}

func notTerminating() int {
	for {
		atoi("") or break // a break referring to the for
	}
	return 0
}
`
	got, info := orErrors(t, src)
	if len(got) > 0 {
		t.Fatalf("unexpected errors:\n%s", strings.Join(got, "\n"))
	}
	// Each or statement has an implicit err of type error and a scope
	// that starts at or.
	n := 0
	for node, obj := range info.Implicits {
		if or, ok := node.(*ast.OrStmt); ok {
			n++
			if obj.Name() != "err" || obj.Type().String() != "error" || obj.Pos() != or.Or {
				t.Errorf("implicit object %v at %d, want err of type error at %d", obj, obj.Pos(), or.Or)
			}
			if sc := info.Scopes[or]; sc == nil || sc.Pos() != or.Or || sc.Lookup("err") != obj {
				t.Errorf("or statement at %d: scope %v", or.Pos(), sc)
			}
		}
	}
	if n < 20 {
		t.Errorf("found %d or statements with an implicit err, want at least 20", n)
	}
}

// TestOrErrors checks the errors of the or statement (language spec 11.27).
func TestOrErrors(t *testing.T) {
	testguard.Parallel(t)
	tests := []struct {
		src  string
		want []string // "line: code message" prefixes; the function starts on line 2
	}{
		{`func f() (int, error) {
	c := int64(one()) or return
	use(c)
	return 0, nil
}`, []string{"3: E2461 int64(one()) is a conversion"}},
		{`func f() (int, error) {
	len("x") or return
	return 0, nil
}`, []string{"3: E2461 len(\"x\") is a call of the built-in function len"}},
		{`func f() (int, error) {
	d := one() or return
	e := mine() or return
	g := impl() or return
	none() or return
	use(d, e, g)
	return 0, nil
}`, []string{
			"3: E2462 the last result of one() has type int, not error",
			"4: E2462 the last result of mine() has type myErr, not error",
			"5: E2462 the last result of impl() has type E, not error",
			"6: E2462 none() returns no result",
		}},
		{`func f() (int, error) {
	load("") or use(err)
	load("") or {
		use(err)
	}
	load("") or exit()
	load("") or {
		if true {
			return 0, err
		}
	}
	load("") or {
		for {
			break
		}
	}
	return 0, nil
}`, []string{
			"3: E2463 the or clause can complete",
			"4: E2463",
			"7: E2463",
			"8: E2463",
			"13: E2463",
		}},
		{`func f() {
	fail() or return
}

func g() int {
	fail() or return
	return 0
}`, []string{
			"3: E2464 the function has no results to carry err",
			"7: E2464 the function has no result of type error last",
		}},
		{`func f() (int, error) {
	for i := 0; i < 3; i = pair() or break {
	}
	return 0, nil
}`, []string{"3: E2465 an or statement cannot be the post statement"}},
		{`func f(s string) (int, error) {
	n, err := atoi(s) or return
	x := fail() or return
	a, b, c := two() or return
	use(n, err, x, a, b, c)
	return 0, nil
}`, []string{
			"3: E0017 assignment mismatch: 2 variables but atoi(s) returns 1 value besides the error",
			"4: E0017 assignment mismatch: 1 variable but fail() returns only an error",
			"5: E0017 assignment mismatch: 3 variables but two() returns 2 values besides the error",
		}},
		{`func f(path string) (int, error) {
	cfg := load(path) or {
		use(cfg.name)
		return 0, err
	}
	use(cfg, err)
	return 0, nil
}`, []string{
			"4: E0075 undefined: cfg",
			"7: E0075 undefined: err",
		}},
		{`func f(path string) (int, error) {
	load(err.Error()) or return
	return 0, nil
}`, []string{"3: E0075 undefined: err"}},
		{`func f() (n int, err error) {
	{
		n := 1
		fail() or return
		use(n)
	}
	return
}`, []string{"5: E0104 result parameter n not in scope at return", "4: E0104 \tinner declaration of var n int"}},
		{`func f() {
	atoi("") or break
	atoi("") or continue
	if v := atoi("") or break; v > 0 {
	}
	for v := atoi("") or continue; v < 3; v++ {
	}
}`, []string{
			"3: E0110 break not in for, switch, or select statement",
			"4: E0111 continue not in for statement",
			"5: E0110 break not in for, switch, or select statement",
			"7: E0111 continue not in for statement",
		}},
		{`func f() {
L:
	for v := atoi("") or break L; v < 3; v++ {
	}
	goto M
	n := atoi("") or goto M
	use(n)
M:
}`, []string{
			"4: E0121 invalid break label L",
			"6: E0123 goto M jumps over variable declaration at line 7",
			"7: E0123 goto M jumps over variable declaration at line 7",
			"3: E0122 label L declared and not used",
		}},
		{`func f() int {
	for {
		atoi("") or break
	}
}`, []string{"6: E0102 missing return"}},
		{`func f() int {
	for {
		if v := atoi("") or break; v > 0 {
		}
	}
}`, []string{"7: E0102 missing return"}},
		{`func f() int {
	for {
		switch v := atoi("") or break; v {
		}
	}
}`, []string{"7: E0102 missing return"}},
		{`func f() (int, error) {
	pair() or return "x"
	return 0, nil
}`, []string{"3: E0021 cannot use \"x\""}},
	}
	for _, tt := range tests {
		got, _ := orErrors(t, tt.src)
		if len(got) != len(tt.want) {
			t.Errorf("%s:\ngot\n%s\nwant\n%s", tt.src, strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			continue
		}
		for i, w := range tt.want {
			if !strings.HasPrefix(got[i], w) {
				t.Errorf("%s:\nerror %d is %q, want prefix %q", tt.src, i, got[i], w)
			}
		}
	}
}
