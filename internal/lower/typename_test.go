package lower

import (
	"testing"

	"github.com/matjam/plutonium/internal/types2"
)

// TestReflectName checks type descriptor names against what Go's
// reflect.Type.String prints for the same types.
func TestReflectName(t *testing.T) {
	pkg := types2.NewPackage("example.com/main", "main")
	newNamed := func(name string, under types2.Type) *types2.Named {
		return types2.NewNamed(types2.NewTypeName(0, pkg, name, nil), under, nil)
	}
	intT, strT := types2.Typ[types2.Int], types2.Typ[types2.String]
	field := func(name string, T types2.Type, embedded bool) *types2.Var {
		return types2.NewField(0, pkg, name, T, embedded)
	}
	point := newNamed("point", types2.NewStruct([]*types2.Var{field("X", intT, false)}, nil))
	param := func(T types2.Type) *types2.Var { return types2.NewParam(0, pkg, "", T) }
	errT := types2.Universe.Lookup("error").Type()
	sig := types2.NewSignatureType(nil, nil, nil,
		types2.NewTuple(param(intT), param(types2.NewSlice(strT))),
		types2.NewTuple(param(types2.Typ[types2.Bool]), param(errT)), true)
	method := types2.NewFunc(0, pkg, "Read", types2.NewSignatureType(nil, nil, nil,
		types2.NewTuple(param(types2.NewSlice(types2.Typ[types2.Byte]))), types2.NewTuple(param(intT)), false))
	iface := types2.NewInterfaceType([]*types2.Func{method}, nil)
	iface.Complete()

	cases := []struct {
		T    types2.Type
		want string
	}{
		{intT, "int"},
		{types2.Typ[types2.Byte], "uint8"},
		{types2.Typ[types2.Rune], "int32"},
		{types2.Typ[types2.UnsafePointer], "unsafe.Pointer"},
		{types2.NewPointer(point), "*main.point"},
		{types2.NewArray(types2.NewSlice(point), 3), "[3][]main.point"},
		{types2.NewMap(strT, intT), "map[string]int"},
		{types2.NewStruct(nil, nil), "struct {}"},
		{types2.NewStruct([]*types2.Var{field("A", intT, false), field("point", point, true)}, []string{`json:"a"`}), `struct { A int "json:\"a\""; main.point }`},
		{types2.NewInterfaceType(nil, nil), "interface {}"},
		{iface, "interface { Read([]uint8) int }"},
		{sig, "func(int, ...string) (bool, error)"},
		{types2.NewSignatureType(nil, nil, nil, nil, types2.NewTuple(param(intT)), false), "func() int"},
		{errT, "error"},
		{types2.NewChan(types2.SendRecv, intT), "chan int"},
	}
	for _, c := range cases {
		if got := reflectName(c.T); got != c.want {
			t.Errorf("reflectName(%s) = %q, want %q", c.T, got, c.want)
		}
	}
}
