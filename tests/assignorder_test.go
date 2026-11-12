package tests

import (
	"strings"
	"testing"
)

// TestTupleAssignOrder checks the two phases of a tuple assignment
// (language spec section 12.7): the operands of the left side's index
// expressions and pointer indirections and the right side are evaluated
// first, then the assignments are carried out left to right, each with its
// own nil and bounds checks. A deferred call prints what the stores before
// the panic left behind; the test compares the output up to the panic
// message, at -O0 and -O2.
func TestTupleAssignOrder(t *testing.T) {
	t.Parallel()
	const header = `package main

type Big struct {
	pad [8192]byte
	v   int
}

var a [3]int
var g int

func two() (int, int) { return 4, 5 }

func three() (int, int, error) { return 6, 7, nil }

func show(x []int) { println(x[0], x[1], x[2], a[0], g) }

`
	cases := []struct {
		name, body, want string
	}{
		{"slice", `x := []int{1, 2, 3}
	defer show(x)
	x[1], x[3] = 4, 5`,
			"1 4 3 0 0\npanic: runtime error: index out of range [3] with length 3\n"},
		{"array", `x := []int{1, 2, 3}
	defer show(x)
	i := 5
	a[0], a[i] = 4, 5`,
			"1 2 3 4 0\npanic: runtime error: index out of range [5] with length 3\n"},
		{"variable", `x := []int{1, 2, 3}
	defer show(x)
	g, x[3] = 4, 5`,
			"1 2 3 0 4\npanic: runtime error: index out of range [3] with length 3\n"},
		{"middle", `x := []int{1, 2, 3}
	defer show(x)
	x[0], x[5], x[1] = 7, 8, 9`,
			"7 2 3 0 0\npanic: runtime error: index out of range [5] with length 3\n"},
		{"right-first", `x := []int{1, 2, 3}
	defer show(x)
	x[9], x[0] = 1, x[8]`,
			"1 2 3 0 0\npanic: runtime error: index out of range [8] with length 3\n"},
		{"array-pointer-bounds", `x := []int{1, 2, 3}
	defer show(x)
	var arr [4]int
	pa := &arr
	i := 7
	x[0], pa[i] = 9, 1`,
			"9 2 3 0 0\npanic: runtime error: index out of range [7] with length 4\n"},
		{"array-pointer-nil", `x := []int{1, 2, 3}
	defer show(x)
	var pa *[4]int
	i := 1
	x[0], pa[i] = 9, 1`,
			"9 2 3 0 0\npanic: runtime error: invalid memory address or nil pointer dereference\n"},
		{"map-nil", `x := []int{1, 2, 3}
	defer show(x)
	var m map[string]int
	x[0], m["k"] = 7, 8`,
			"7 2 3 0 0\npanic: assignment to entry in nil map\n"},
		{"deref-nil", `x := []int{1, 2, 3}
	defer show(x)
	var p *int
	x[0], *p = 7, 8`,
			"7 2 3 0 0\npanic: runtime error: invalid memory address or nil pointer dereference\n"},
		{"far-field-nil", `x := []int{1, 2, 3}
	defer show(x)
	var bp *Big
	x[0], bp.v = 7, 8`,
			"7 2 3 0 0\npanic: runtime error: invalid memory address or nil pointer dereference\n"},
		{"nested-slice", `x := []int{1, 2, 3}
	defer show(x)
	s := [][]int{x, {4}}
	s[0][0], s[1][9] = 7, 8`,
			"7 2 3 0 0\npanic: runtime error: index out of range [9] with length 1\n"},
		{"inner-operand", `x := []int{1, 2, 3}
	defer show(x)
	s := [][]int{x, {4}}
	x[0], s[5][0] = 7, 8`,
			"1 2 3 0 0\npanic: runtime error: index out of range [5] with length 2\n"},
		{"call", `x := []int{1, 2, 3}
	defer show(x)
	x[1], x[5] = two()`,
			"1 4 3 0 0\npanic: runtime error: index out of range [5] with length 3\n"},
		{"or", `x := []int{1, 2, 3}
	defer show(x)
	x[2], x[5] = three() or {
		return
	}`,
			"1 2 6 0 0\npanic: runtime error: index out of range [5] with length 3\n"},
	}
	for _, c := range cases {
		src := header + "func main() {\n\t" + c.body + "\n}\n"
		for _, level := range []int{0, 2} {
			t.Run(c.name+"/"+strings.TrimPrefix(optFlag(level), "-"), func(t *testing.T) {
				t.Parallel()
				_, out, status := buildAndRun(t, map[string][]byte{"main.p": []byte(src)}, optFlag(level))
				if status != 2 {
					t.Errorf("exit status %d, want 2\n%s", status, out)
				}
				got := out
				if i := strings.Index(got, "panic: "); i >= 0 {
					if j := strings.IndexByte(got[i:], '\n'); j >= 0 {
						got = got[:i+j+1]
					}
				}
				if got != c.want {
					t.Errorf("output differs\n--- got ---\n%s--- want ---\n%s", got, c.want)
				}
			})
		}
	}
}
