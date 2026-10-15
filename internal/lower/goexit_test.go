package lower

import (
	"testing"

	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// TestGoexitSigMatch checks how the rt.ExitTask analysis matches the type of
// an indirect call against the functions that may end with Goexit: types
// must be identical except where either side has a type parameter, which
// matches anything.
func TestGoexitSigMatch(t *testing.T) {
	tp := types2.NewTypeParam(types2.NewTypeName(token.NoPos, nil, "T", nil), types2.NewInterfaceType(nil, nil))
	i, s := types2.Typ[types2.Int], types2.Typ[types2.String]
	named := func(name string) *types2.Named {
		return types2.NewNamed(types2.NewTypeName(token.NoPos, nil, name, nil), types2.NewStruct(nil, nil), nil)
	}
	n1, n2 := named("A"), named("B")
	sig := func(variadic bool, results []types2.Type, params ...types2.Type) *types2.Signature {
		tuple := func(ts []types2.Type) *types2.Tuple {
			var vs []*types2.Var
			for _, T := range ts {
				vs = append(vs, types2.NewParam(token.NoPos, nil, "", T))
			}
			return types2.NewTuple(vs...)
		}
		return types2.NewSignatureType(nil, nil, nil, tuple(params), tuple(results), variadic)
	}
	tests := []struct {
		name string
		a, b types2.Type
		want bool
	}{
		{"identical basic", i, i, true},
		{"different basic", i, s, false},
		{"type parameter left", tp, s, true},
		{"type parameter right", i, tp, true},
		{"pointer", types2.NewPointer(tp), types2.NewPointer(i), true},
		{"pointer and slice", types2.NewPointer(i), types2.NewSlice(i), false},
		{"slice", types2.NewSlice(tp), types2.NewSlice(s), true},
		{"slice and array", types2.NewSlice(i), types2.NewArray(i, 2), false},
		{"array", types2.NewArray(tp, 3), types2.NewArray(i, 3), true},
		{"array length", types2.NewArray(i, 3), types2.NewArray(i, 4), false},
		{"array and chan", types2.NewArray(i, 3), types2.NewChan(types2.SendRecv, i), false},
		{"chan", types2.NewChan(types2.SendRecv, tp), types2.NewChan(types2.SendRecv, i), true},
		{"chan direction", types2.NewChan(types2.SendOnly, i), types2.NewChan(types2.SendRecv, i), false},
		{"chan and pointer", types2.NewChan(types2.SendRecv, i), types2.NewPointer(i), false},
		{"func", sig(false, nil, tp), sig(false, nil, i), true},
		{"func results", sig(false, []types2.Type{i}, i), sig(false, nil, i), false},
		{"func variadic", sig(true, nil, types2.NewSlice(i)), sig(false, nil, types2.NewSlice(i)), false},
		{"func and basic", sig(false, nil), i, false},
		{"named", n1, n1, true},
		{"different named", n1, n2, false},
		{"named and basic", n1, i, false},
	}
	for _, tt := range tests {
		if got := gxTypeMatch(tt.a, tt.b, 0); got != tt.want {
			t.Errorf("%s: gxTypeMatch(%s, %s) = %v, want %v", tt.name, tt.a, tt.b, got, tt.want)
		}
	}
	if !gxTypeMatch(types2.NewPointer(i), types2.NewPointer(s), 9) {
		t.Errorf("past the depth limit, types should be taken to match")
	}
	if gxSigMatch(sig(false, nil, i), sig(false, nil, i, i)) {
		t.Errorf("signatures with different numbers of parameters match")
	}
	if !gxSigMatch(sig(false, []types2.Type{tp}, tp), sig(false, []types2.Type{s}, i)) {
		t.Errorf("a generic signature does not match an instance")
	}
}
