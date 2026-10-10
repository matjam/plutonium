package build

import "testing"

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
	for _, c := range cases {
		if got := c.cfg.fieldNames(); got != c.want {
			t.Errorf("Kernel=%v FieldNames=%q: fieldNames() = %v, want %v", c.cfg.Kernel, c.cfg.FieldNames, got, c.want)
		}
	}
}
