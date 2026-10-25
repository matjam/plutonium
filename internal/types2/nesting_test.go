package types2_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// checkWith type-checks src with conf and returns every error message.
func checkWith(t *testing.T, conf types2.Config, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.p", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var errs []string
	conf.Importer = unsafeOnly{}
	conf.Error = func(err error) { errs = append(errs, err.Error()) }
	_, _ = conf.Check("test", fset, []*ast.File{f}, nil)
	return errs
}

// TestNestingLimit checks that expressions and type expressions nested
// deeper than Config.MaxNesting, or deeper than Config.StackCheck allows,
// are reported once as too deep instead of being checked.
func TestNestingLimit(t *testing.T) {
	deepExpr := "package p\nvar x = " + strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200) + "\n"
	deepType := "package p\nvar y " + strings.Repeat("[]", 200) + "int\n"
	shallow := "package p\nvar z = " + strings.Repeat("(", 20) + "1" + strings.Repeat(")", 20) + "\n"

	for _, c := range []struct {
		name string
		src  string
		conf types2.Config
		want int // number of nesting errors
	}{
		{"expr over limit", deepExpr, types2.Config{MaxNesting: 100}, 1},
		{"type over limit", deepType, types2.Config{MaxNesting: 100}, 1},
		{"both over limit", deepExpr + deepType[len("package p\n"):], types2.Config{MaxNesting: 100}, 1},
		{"under limit", shallow, types2.Config{MaxNesting: 100}, 0},
		{"default limit", deepExpr, types2.Config{}, 0},
		{"stack check stops", deepExpr, types2.Config{StackCheck: stackStop{}}, 1},
		{"stack check allows", deepExpr, types2.Config{StackCheck: stackAllow{}}, 0},
		{"stack check on types", deepType, types2.Config{StackCheck: stackStop{}}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := checkWith(t, c.conf, c.src)
			n := 0
			for _, e := range errs {
				if strings.Contains(e, "nested too deeply") {
					n++
				} else {
					t.Errorf("unexpected error: %s", e)
				}
			}
			if n != c.want {
				t.Errorf("%d nesting errors, want %d: %v", n, c.want, errs)
			}
		})
	}
}

// stackStop and stackAllow are StackCheckers that report the stack as
// running out and as never running out.
type (
	stackStop  struct{}
	stackAllow struct{}
)

func (stackStop) StackOK() bool  { return false }
func (stackAllow) StackOK() bool { return true }
