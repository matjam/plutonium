// Adapted from Go's cmp/cmp_test.go. Go's table holds x and y as any and
// switches on their type; here there is a table for each type.

package cmp

import (
	"pluto/math"
	"pluto/testing"
)

var negzero = math.Copysign(0, -1)

var intTests = [...]struct {
	x, y    int
	compare int
}{
	{1, 2, -1},
	{1, 1, 0},
	{2, 1, +1},
}

var stringTests = [...]struct {
	x, y    string
	compare int
}{
	{"a", "aa", -1},
	{"a", "a", 0},
	{"aa", "a", +1},
}

var floatTests = [...]struct {
	x, y    float64
	compare int
}{
	{1.0, 1.1, -1},
	{1.1, 1.1, 0},
	{1.1, 1.0, +1},
	{math.Inf(1), math.Inf(1), 0},
	{math.Inf(-1), math.Inf(-1), 0},
	{math.Inf(-1), 1.0, -1},
	{1.0, math.Inf(-1), +1},
	{math.Inf(1), 1.0, +1},
	{1.0, math.Inf(1), -1},
	{math.NaN(), math.NaN(), 0},
	{0.0, math.NaN(), +1},
	{math.NaN(), 0.0, -1},
	{math.NaN(), math.Inf(-1), -1},
	{math.Inf(-1), math.NaN(), +1},
	{0.0, 0.0, 0},
	{negzero, negzero, 0},
	{negzero, 0.0, 0},
	{0.0, negzero, 0},
	{negzero, 1.0, -1},
	{negzero, -1.0, +1},
}

var uintptrTests = [...]struct {
	x, y    uintptr
	compare int
}{
	{0, 8, -1},
	{8, 0, 1},
	{8, 8, 0},
}

func TestLess(t *testing.T) {
	for _, test := range intTests {
		if b := Less(test.x, test.y); b != (test.compare < 0) {
			t.Errorf("Less(%v, %v) == %t, want %t", test.x, test.y, b, test.compare < 0)
		}
	}
	for _, test := range stringTests {
		if b := Less(test.x, test.y); b != (test.compare < 0) {
			t.Errorf("Less(%q, %q) == %t, want %t", test.x, test.y, b, test.compare < 0)
		}
	}
	for _, test := range floatTests {
		if b := Less(test.x, test.y); b != (test.compare < 0) {
			t.Errorf("Less(%v, %v) == %t, want %t", test.x, test.y, b, test.compare < 0)
		}
	}
	for _, test := range uintptrTests {
		if b := Less(test.x, test.y); b != (test.compare < 0) {
			t.Errorf("Less(%v, %v) == %t, want %t", test.x, test.y, b, test.compare < 0)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, test := range intTests {
		if c := Compare(test.x, test.y); c != test.compare {
			t.Errorf("Compare(%v, %v) == %d, want %d", test.x, test.y, c, test.compare)
		}
	}
	for _, test := range stringTests {
		if c := Compare(test.x, test.y); c != test.compare {
			t.Errorf("Compare(%q, %q) == %d, want %d", test.x, test.y, c, test.compare)
		}
	}
	for _, test := range floatTests {
		if c := Compare(test.x, test.y); c != test.compare {
			t.Errorf("Compare(%v, %v) == %d, want %d", test.x, test.y, c, test.compare)
		}
	}
	for _, test := range uintptrTests {
		if c := Compare(test.x, test.y); c != test.compare {
			t.Errorf("Compare(%v, %v) == %d, want %d", test.x, test.y, c, test.compare)
		}
	}
}

func TestOr(t *testing.T) {
	cases := [...]struct {
		in   []int
		want int
	}{
		{nil, 0},
		{[]int{0}, 0},
		{[]int{1}, 1},
		{[]int{0, 2}, 2},
		{[]int{3, 0}, 3},
		{[]int{4, 5}, 4},
		{[]int{0, 6, 7}, 6},
	}
	for _, tc := range cases {
		if got := Or(tc.in...); got != tc.want {
			t.Errorf("Or(%v) = %v; want %v", tc.in, got, tc.want)
		}
	}
	if got := Or("", "some text", "default"); got != "some text" {
		t.Errorf("Or of strings = %q", got)
	}
}
