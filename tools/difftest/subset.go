package main

import "strings"

// The generator writes programs in the common subset of Go and Plutonium:
// programs both compilers accept and give the same meaning. Plutonium is
// still changing, so the parts of the subset that move are switched here,
// in one place.
//
// Never generated, because Plutonium rejects them or means something else
// by them (docs/plans/2026-10-10-go-false-friends.md):
//
//   - append, and make([]T, n): slices are slices of arrays or composite
//     literals (both are being removed in favour of pluto/vec);
//   - conversions from []byte to string (removed, F2, E2015), and from a
//     string to []byte or []rune (removed, G10, E2016), and string(i) of an
//     integer that is not a rune (removed, G11, E2017);
//   - maps (removed until map[K]V returns);
//   - new, and pointers or slices of a function's frame that outlive it:
//     functions return values, never views (spec section 6.3);
//   - a function value, pointer or interface box made in a loop body and
//     kept past the iteration (F1);
//   - goroutines, channels, recover, complex numbers, reflect.
type subsetRules struct {
	// InvalidUTF8 lets string literals spell bytes that are not valid
	// UTF-8. Plutonium's strings always hold valid UTF-8 (E2012).
	InvalidUTF8 bool
	// ByteSlicing lets strings be sliced at any byte offset. Plutonium
	// panics on a bound that is not at the start of a rune.
	ByteSlicing bool
	// ReturnedClosures lets functions return closures, which Plutonium
	// builds in regions their callers provide.
	ReturnedClosures bool
	// RangeFunc lets loops range over functions (Go 1.23).
	RangeFunc bool
}

var subset = subsetRules{
	InvalidUTF8:      false,
	ByteSlicing:      false,
	ReturnedClosures: true,
	RangeFunc:        true,
}

// outsideSubset lists text in Plutonium's output that means a program is
// outside the subset: a generator bug, unless the compiler applied the rule
// to a program that keeps it, which the reproducer shows.
var outsideSubset = []string{
	"is not valid UTF-8",                                         // E2012, an invalid string literal
	"is not at the start of a rune",                              // the panic of a string slice
	"string(b) of a byte slice is not permitted",                 // E2015
	"[]byte(s) is not permitted",                                 // E2016
	"[]rune(s) is not permitted",                                 // E2016
	"string(i) of an integer other than a rune is not permitted", // E2017
}

// leftSubset returns the line of out that shows the program is outside the
// subset, or "".
func leftSubset(out string) string {
	for _, line := range strings.Split(out, "\n") {
		for _, s := range outsideSubset {
			if strings.Contains(line, s) {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}
