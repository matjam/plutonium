package types2_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
	"github.com/matjam/plutonium/internal/types2"
)

func TestOwnershipKind(t *testing.T) {
	testguard.Parallel(t)
	pkg, _, err := checkSrc(t, `package kinds
import "unsafe"
type Own owner struct{ Data []byte }
type OwnIface owner interface{ Close() }
type Alias = OwnIface
type Error interface{ Error() string }
type Mixed struct { Text string; Data []byte }
type Counted struct { Text string; Ref shared[int] }
type Text union { Empty; Some string }
type MixedUnion union { Empty; Some string; Bytes []byte }
type Enum enum { First; Second }
type Bits packed uint8 { Low bits[uint8, 4]; High bits[uint8, 4] }
type Plain int
type Generic[T any] struct { V T }
type GenericOwner[T owner any] owner struct { V T }
var String string
var Map map[int]int
var Chan chan int
var Fn func()
var Slice []int
var Pointer *int
var Unsafe unsafe.Pointer
var Shared shared[int]
var Synced synced[int]
var Weak weak[shared[int]]
var Owned owned[int]
var Array [4]string
var Volatile volatile[uint32]
var Aligned aligned[string, 8]
var Ring ring[string, 8]
var Ordinary Generic[int]
var Owner GenericOwner[Own]
type Recursive struct { Next *Recursive; Text string }
`)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]types2.OwnershipKind{
		"Own": types2.KindOwner, "OwnIface": types2.KindOwner, "Alias": types2.KindOwner,
		"Error": types2.KindView, "Mixed": types2.KindView, "Counted": types2.KindCounted,
		"Text": types2.KindCounted, "MixedUnion": types2.KindView, "Enum": types2.KindValue,
		"Bits": types2.KindValue, "Plain": types2.KindValue, "String": types2.KindCounted,
		"Map": types2.KindCounted, "Chan": types2.KindCounted, "Fn": types2.KindView,
		"Slice": types2.KindView, "Pointer": types2.KindView, "Unsafe": types2.KindView,
		"Shared": types2.KindCounted, "Synced": types2.KindCounted, "Weak": types2.KindCounted,
		"Owned": types2.KindOwner, "Array": types2.KindCounted, "Volatile": types2.KindValue,
		"Aligned": types2.KindCounted, "Ring": types2.KindCounted, "Ordinary": types2.KindValue,
		"Owner": types2.KindOwner, "Recursive": types2.KindView,
	} {
		if got := types2.KindOf(pkg.Scope().Lookup(name).Type()); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	for _, tc := range []struct {
		name string
		want types2.OwnershipKind
	}{
		{"Generic", types2.KindValue}, {"GenericOwner", types2.KindOwner},
	} {
		n := pkg.Scope().Lookup(tc.name).Type().(*types2.Named)
		if got := types2.KindOf(n.TypeParams().At(0)); got != tc.want {
			t.Errorf("%s parameter = %s", tc.name, got)
		}
	}
	// Documentation must not infer ownership from method names, even when
	// inspecting incomplete editor types which have not passed owner checks.
	plain := pkg.Scope().Lookup("Plain").Type().(*types2.Named)
	plain.AddMethod(types2.NewFunc(0, pkg, "Drop", types2.NewSignatureType(types2.NewVar(0, pkg, "p", types2.NewPointer(plain)), nil, nil, nil, nil, false)))
	if got := types2.KindOf(plain); got != types2.KindValue {
		t.Errorf("ordinary Drop method inferred %s", got)
	}
	deep := types2.Type(types2.Typ[types2.String])
	for range 64 {
		deep = types2.NewArray(deep, 1)
	}
	if got := types2.KindOf(deep); got != types2.KindCounted {
		t.Errorf("deep arrays = %s", got)
	}
	for _, invalid := range []types2.Type{nil, types2.Typ[types2.Invalid], types2.Typ[types2.UntypedNil]} {
		if got := types2.KindOf(invalid); got != types2.KindUnknown {
			t.Errorf("invalid = %s", got)
		}
	}
	for kind, want := range map[types2.OwnershipKind]string{
		types2.KindValue: "value", types2.KindView: "view", types2.KindOwner: "owner (moves)", types2.KindCounted: "counted", types2.KindUnknown: "",
	} {
		if got := kind.String(); got != want {
			t.Errorf("label %d = %q", kind, got)
		}
	}
}
