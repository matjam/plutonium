package types2_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/types2"
)

func TestIsCountedError(t *testing.T) {
	pkg, _, err := checkSrc(t, `package p
type embedded interface { error; Context() string }
type structural interface { Error() string }
type unrelated interface { Error() int }
type owning owner interface { Error() string }
type concrete struct{}
func (*concrete) Error() string { return "error" }
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"embedded", "structural", "unrelated", "owning", "concrete"} {
		T := pkg.Scope().Lookup(name).Type()
		want := name == "embedded" || name == "structural"
		if got := types2.IsCountedError(T); got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	if types2.IsCountedError(nil) || types2.IsCountedError(types2.NewInterfaceType(nil, nil)) {
		t.Fatal("nil and the empty interface are not counted errors")
	}
	if !types2.IsCountedError(types2.Universe.Lookup("error").Type()) {
		t.Fatal("the predeclared error interface is counted")
	}
}
