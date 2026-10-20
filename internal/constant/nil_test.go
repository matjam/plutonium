package constant

import "testing"

// TestIsNil checks that the zero Value, which stands for a nil go/constant
// Value, differs from the Unknown value, and that both have kind Unknown.
func TestIsNil(t *testing.T) {
	var zero Value
	if !zero.IsNil() || zero.Kind() != Unknown {
		t.Errorf("zero Value: IsNil %v, kind %v", zero.IsNil(), zero.Kind())
	}
	u := MakeUnknown()
	if u.IsNil() || u.Kind() != Unknown || u == zero {
		t.Errorf("MakeUnknown: IsNil %v, kind %v, equal to zero %v", u.IsNil(), u.Kind(), u == zero)
	}
	// Operations that fail give Unknown, not the zero Value.
	for name, v := range map[string]Value{
		"ToInt of a string": ToInt(MakeString("x")),
		"ToFloat of a bool": ToFloat(MakeBool(true)),
	} {
		if v.IsNil() || v.Kind() != Unknown {
			t.Errorf("%s: IsNil %v, kind %v", name, v.IsNil(), v.Kind())
		}
	}
	for _, v := range []Value{MakeBool(false), MakeString(""), MakeInt64(0), MakeFloat64(0)} {
		if v.IsNil() {
			t.Errorf("%v is nil", v)
		}
	}
}
