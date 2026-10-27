// Ported from internal/build/fieldnames_internal_test.go at the go-cutover tag.

package build

import "pluto/testing"

// TestFieldNamesDefault checks that type descriptors keep field names by
// default in hosted and freestanding builds and leave them out in kernel
// mode, unless --fieldnames says otherwise.
func TestFieldNamesDefault(t *testing.T) {
	cases := []struct {
		cfg  Config
		want bool
	}{
		{Config{}, true},
		{Config{Freestanding: true}, true},
		{Config{Kernel: true}, false},
		{Config{Kernel: true, FieldNames: "on"}, true},
		{Config{FieldNames: "off"}, false},
	}
	for i := range cases {
		c := &cases[i]
		if got := c.cfg.fieldNames(); got != c.want {
			t.Errorf("Kernel=%v FieldNames=%q: fieldNames() = %v, want %v", c.cfg.Kernel, c.cfg.FieldNames, got, c.want)
		}
	}
}
