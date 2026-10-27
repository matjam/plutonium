// Ported from internal/escape2/solve/boxneeded_test.go at the go-cutover tag.

package solve

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/types2"
)

// TestDirectValue: the instances of a type parameter whose conversion to an
// interface makes no box. An interface instance is converted by taking its
// data word (a conversion between interfaces), so it is direct as a pointer
// is; a string or a struct of two words is not.
func TestDirectValue(t *testing.T) {
	empty := types2.NewInterfaceType(nil, nil)
	twoWords := types2.NewStruct(compat.Append[*types2.Var](nil,
		types2.NewField(0, nil, "a", types2.Typ[types2.Int], false),
		types2.NewField(0, nil, "b", types2.Typ[types2.Int], false),
	), nil)
	for _, c := range []struct {
		T    types2.Type
		want bool
	}{
		{types2.NewPointer(types2.Typ[types2.Int]), true},
		{types2.Typ[types2.Int], true},
		{empty, true},
		{types2.Typ[types2.String], false},
		{twoWords, false},
	} {
		if got := directValue(c.T); got != c.want {
			t.Errorf("directValue(%s) = %v, want %v", types2.TypeString(c.T, nil), got, c.want)
		}
	}
}
