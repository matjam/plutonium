package types2_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Sections 15.2 and 15.4 require zero-initialized slots to retain their actual
// ready-queue positions, not be appended after the emitted initializers.
func TestVariableLifetimeInitializationOrder(t *testing.T) {
	testguard.Parallel(t)
	for _, tc := range []struct {
		name, source, variables, initializers string
	}{
		{"interleavedZero", `package p
var first int
var second = 2
var third int
`, "first second third", "second"},
		{"zeroDependency", `package p
var early = read()
var middle int
var late int
func read() int { return late }
`, "middle late early", "early"},
		{"groupDependency", `package p
var first = b
var a, b = pair()
var last int
func pair() (int, int) { return 1, 2 }
`, "a b first last", "a,b first"},
		{"blankAndConstants", `package p
const c = 1
var first, second int
var _, third = pair()
var fourth = c
func pair() (int, int) { return 1, 2 }
`, "first second _ third fourth", "_,third fourth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testguard.Parallel(t)
			_, info := mustCheck(t, tc.source)
			var variables, initializers []string
			for _, v := range info.VarInitOrder {
				variables = append(variables, v.Name())
			}
			for _, init := range info.InitOrder {
				var lhs []string
				for _, v := range init.Lhs {
					lhs = append(lhs, v.Name())
				}
				initializers = append(initializers, strings.Join(lhs, ","))
			}
			if got := strings.Join(variables, " "); got != tc.variables {
				t.Errorf("complete variable order %q; want %q", got, tc.variables)
			}
			if got := strings.Join(initializers, " "); got != tc.initializers {
				t.Errorf("initializer-only order changed: %q; want %q", got, tc.initializers)
			}
		})
	}
}
