package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// buildError compiles src as a main package and returns the build error.
func buildError(t *testing.T, src string) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib()}
	return build.Build(cfg)
}

// TestGenericMethodErrors checks that the restrictions on methods with type
// parameters are reported as compile errors: such methods are called
// statically only, so they never implement an interface, an interface
// cannot declare one, and a method value or method expression must be
// instantiated.
func TestGenericMethodErrors(t *testing.T) {
	t.Parallel()
	const decls = `package main

type S struct{ x int }

func (s S) Apply[U any](f func(int) U) U { return f(s.x) }

func (s *S) Len[U any](xs []U) int { return len(xs) }

type Lener interface{ Len(xs []int) int }

`
	cases := []struct{ name, src, want string }{
		{"interface declares one", "type I interface {\n\tM[T any]() T\n}\n\nfunc main() {}\n",
			"interface method must have no type parameters: a method with type parameters is never called through an interface"},
		{"implements", "func main() {\n\tvar l Lener = &S{}\n\t_ = l\n}\n",
			"*S does not implement Lener (method Len has type parameters, so it cannot implement an interface method)"},
		{"method value", "func main() {\n\tf := S{}.Apply\n\t_ = f\n}\n",
			"cannot use generic method S{}.Apply without instantiation"},
		{"method expression", "func main() {\n\tf := (*S).Len\n\t_ = f\n}\n",
			"cannot use generic method (*S).Len without instantiation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := buildError(t, decls+c.src)
			if err == nil {
				t.Fatalf("no error, want %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

// TestGenericMethodSymbols checks how instances of methods with type
// parameters are named: the receiver type's arguments go on the receiver
// and the method's own after the method name.
func TestGenericMethodSymbols(t *testing.T) {
	t.Parallel()
	const src = `package main

type Vec[T any] struct{ x T }

//pu:noinline
func (v *Vec[T]) Map[U any](f func(T) U) Vec[U] { return Vec[U]{f(v.x)} }

type S struct{}

//pu:noinline
func (S) Twice[U any](u U) (U, U) { return u, u }

func main() {
	v := Vec[string]{"abc"}
	w := v.Map(func(s string) int { return len(s) })
	a, b := S{}.Twice[int](w.x)
	println(a, b)
}
`
	for _, name := range []string{"main.(*Vec[string]).Map[int]", "main.S.Twice[int]"} {
		if text := listFunc(t, src, name); !strings.Contains(text, "ret") {
			t.Errorf("%s has no return:\n%s", name, text)
		}
	}
}
