package types2_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/types2"
)

// TestPointerShaped checks which types an interface holds in its data word
// as a single pointer (language spec section 4.4).
func TestPointerShaped(t *testing.T) {
	pkg, _ := mustCheck(t, `package p

import "unsafe"

type T struct{ n int }

type (
	ptr      *T
	fn       func(int) int
	up       unsafe.Pointer
	wrap     struct{ p *T }
	wrapFn   struct{ f func() }
	wrapUP   struct{ p unsafe.Pointer }
	nested   struct{ w wrap }
	arr1     [1]*T
	arrWrap  [1]wrap
	own      owned[T]
	wrapOwn  owner struct{ o owned[T] }
	alias    = wrap
	named    wrap
	two      struct{ p, q *T }
	blank    struct{ p *T; _ int }
	scalar   struct{ n int }
	arr2     [2]*T
	arr0     [0]*T
	str      string
	slice    []*T
	iface    interface{ M() }
	wrapI    struct{ i interface{ M() } }
	integer  int
	empty    struct{}
	wrapSlc  struct{ s []byte }
)
`)
	cases := []struct {
		name string
		want bool
	}{
		{"ptr", true},
		{"fn", true},
		{"up", true},
		{"wrap", true},
		{"wrapFn", true},
		{"wrapUP", true},
		{"nested", true},
		{"arr1", true},
		{"arrWrap", true},
		{"own", true},
		{"wrapOwn", true},
		{"alias", true},
		{"named", true},
		{"two", false},
		{"blank", false},
		{"scalar", false},
		{"arr2", false},
		{"arr0", false},
		{"str", false},
		{"slice", false},
		{"iface", false},
		{"wrapI", false},
		{"integer", false},
		{"empty", false},
		{"wrapSlc", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := pkg.Scope().Lookup(c.name)
			if obj == nil {
				t.Fatalf("no type %s", c.name)
			}
			if got := types2.PointerShaped(obj.Type()); got != c.want {
				t.Errorf("PointerShaped(%s) = %v, want %v", obj.Type(), got, c.want)
			}
		})
	}
}
