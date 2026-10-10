// Adapted from Go's slices/slices_test.go and sort_test.go. Go's tests that
// need recover (the panic cases), testing.AllocsPerRun, nil-preserving
// Clone, or closures that outlive their frame are left out; the tests of
// Clone, Concat and Repeat check the Vec they return.

package slices

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/cmp"
	"pluto/math"
	"pluto/testing"
	"pluto/vec"
)

var equalIntTests = [...]struct {
	s1, s2 []int
	want   bool
}{
	{[]int{1}, nil, false},
	{[]int{}, nil, true},
	{[]int{1, 2, 3}, []int{1, 2, 3}, true},
	{[]int{1, 2, 3}, []int{1, 2, 3, 4}, false},
}

var equalFloatTests = [...]struct {
	s1, s2       []float64
	wantEqual    bool
	wantEqualNaN bool
}{
	{[]float64{1, 2}, []float64{1, 2}, true, true},
	{[]float64{1, 2, math.NaN()}, []float64{1, 2, math.NaN()}, false, true},
}

func TestEqual(t *testing.T) {
	for _, test := range equalIntTests {
		if got := Equal(test.s1, test.s2); got != test.want {
			t.Errorf("Equal(%v, %v) = %t, want %t", test.s1, test.s2, got, test.want)
		}
	}
	for _, test := range equalFloatTests {
		if got := Equal(test.s1, test.s2); got != test.wantEqual {
			t.Errorf("Equal(%v, %v) = %t, want %t", test.s1, test.s2, got, test.wantEqual)
		}
	}
}

// equal is simply ==.
func equal[T comparable](v1, v2 T) bool {
	return v1 == v2
}

// equalNaN is like == except that all NaNs are equal.
func equalNaN[T comparable](v1, v2 T) bool {
	return v1 == v2 || (v1 != v1 && v2 != v2)
}

// offByOne returns true if integers v1 and v2 differ by 1.
func offByOne(v1, v2 int) bool {
	return v1 == v2+1 || v1 == v2-1
}

func eqToCmp(v1, v2 int) int {
	if v1 == v2 {
		return 0
	}
	return 1
}

func TestEqualFunc(t *testing.T) {
	for _, test := range equalIntTests {
		if got := EqualFunc(test.s1, test.s2, equal[int]); got != test.want {
			t.Errorf("EqualFunc(%v, %v, equal[int]) = %t, want %t", test.s1, test.s2, got, test.want)
		}
	}
	for _, test := range equalFloatTests {
		if got := EqualFunc(test.s1, test.s2, equal[float64]); got != test.wantEqual {
			t.Errorf("Equal(%v, %v, equal[float64]) = %t, want %t", test.s1, test.s2, got, test.wantEqual)
		}
		if got := EqualFunc(test.s1, test.s2, equalNaN[float64]); got != test.wantEqualNaN {
			t.Errorf("Equal(%v, %v, equalNaN[float64]) = %t, want %t", test.s1, test.s2, got, test.wantEqualNaN)
		}
	}

	s1 := []int{1, 2, 3}
	s2 := []int{2, 3, 4}
	if EqualFunc(s1, s1, offByOne) {
		t.Errorf("EqualFunc(%v, %v, offByOne) = true, want false", s1, s1)
	}
	if !EqualFunc(s1, s2, offByOne) {
		t.Errorf("EqualFunc(%v, %v, offByOne) = false, want true", s1, s2)
	}

	s3 := []string{"a", "b", "c"}
	cmpIntString := func(v1 int, v2 string) bool {
		return rune(v1)-1+'a' == rune(v2[0])
	}
	if !EqualFunc(s1, s3, cmpIntString) {
		t.Errorf("EqualFunc(%v, %v, cmpIntString) = false, want true", s1, s3)
	}
}

var compareIntTests = [...]struct {
	s1, s2 []int
	want   int
}{
	{[]int{1}, []int{1}, 0},
	{[]int{1}, []int{}, 1},
	{[]int{}, []int{1}, -1},
	{[]int{}, []int{}, 0},
	{[]int{1, 2, 3}, []int{1, 2, 3}, 0},
	{[]int{1, 2, 3}, []int{1, 2, 3, 4}, -1},
	{[]int{1, 2, 3, 4}, []int{1, 2, 3}, +1},
	{[]int{1, 2, 3}, []int{1, 4, 3}, -1},
	{[]int{1, 4, 3}, []int{1, 2, 3}, +1},
	{[]int{1, 4, 3}, []int{1, 2, 3, 8, 9}, +1},
}

var compareFloatTests = [...]struct {
	s1, s2 []float64
	want   int
}{
	{[]float64{}, []float64{}, 0},
	{[]float64{1}, []float64{1}, 0},
	{[]float64{math.NaN()}, []float64{math.NaN()}, 0},
	{[]float64{1, 2, math.NaN()}, []float64{1, 2, math.NaN()}, 0},
	{[]float64{1, math.NaN(), 3}, []float64{1, math.NaN(), 4}, -1},
	{[]float64{1, math.NaN(), 3}, []float64{1, 2, 4}, -1},
	{[]float64{1, math.NaN(), 3}, []float64{1, 2, math.NaN()}, -1},
	{[]float64{1, 2, 3}, []float64{1, 2, math.NaN()}, +1},
	{[]float64{1, 2, 3}, []float64{1, math.NaN(), 3}, +1},
	{[]float64{1, math.NaN(), 3, 4}, []float64{1, 2, math.NaN()}, -1},
}

func TestCompare(t *testing.T) {
	for _, test := range equalIntTests {
		if got := Compare(test.s1, test.s2); (got == 0) != test.want {
			t.Errorf("Compare(%v, %v) = %d, want zero %t", test.s1, test.s2, got, test.want)
		}
	}
	for _, test := range equalFloatTests {
		if got := Compare(test.s1, test.s2); (got == 0) != test.wantEqualNaN {
			t.Errorf("Compare(%v, %v) = %d, want zero %t", test.s1, test.s2, got, test.wantEqualNaN)
		}
	}
	for _, test := range compareIntTests {
		if got := Compare(test.s1, test.s2); got != test.want {
			t.Errorf("Compare(%v, %v) = %d, want %d", test.s1, test.s2, got, test.want)
		}
	}
	for _, test := range compareFloatTests {
		if got := Compare(test.s1, test.s2); got != test.want {
			t.Errorf("Compare(%v, %v) = %d, want %d", test.s1, test.s2, got, test.want)
		}
	}
}

func TestCompareFunc(t *testing.T) {
	for _, test := range equalIntTests {
		if got := CompareFunc(test.s1, test.s2, eqToCmp); (got == 0) != test.want {
			t.Errorf("CompareFunc(%v, %v, eqToCmp) = %d, want zero %t", test.s1, test.s2, got, test.want)
		}
	}
	for _, test := range compareIntTests {
		if got := CompareFunc(test.s1, test.s2, cmp.Compare[int]); got != test.want {
			t.Errorf("CompareFunc(%v, %v, cmp[int]) = %d, want %d", test.s1, test.s2, got, test.want)
		}
	}
	for _, test := range compareFloatTests {
		if got := CompareFunc(test.s1, test.s2, cmp.Compare[float64]); got != test.want {
			t.Errorf("CompareFunc(%v, %v, cmp[float64]) = %d, want %d", test.s1, test.s2, got, test.want)
		}
	}
	s1 := []int{1, 2, 3}
	s2 := []int{2, 3, 4}
	if got := CompareFunc(s1, s2, func(a, b int) int {
		if offByOne(a, b) {
			return 0
		}
		return 1
	}); got != 0 {
		t.Errorf("CompareFunc(%v, %v, offByOne) = %d, want 0", s1, s2, got)
	}
}

var indexTests = [...]struct {
	s    []int
	v    int
	want int
}{
	{nil, 0, -1},
	{[]int{}, 0, -1},
	{[]int{1, 2, 3}, 2, 1},
	{[]int{1, 2, 2, 3}, 2, 1},
	{[]int{1, 2, 3, 2}, 2, 1},
}

func TestIndex(t *testing.T) {
	for _, test := range indexTests {
		if got := Index(test.s, test.v); got != test.want {
			t.Errorf("Index(%v, %v) = %d, want %d", test.s, test.v, got, test.want)
		}
	}
}

func TestIndexFunc(t *testing.T) {
	for _, test := range indexTests {
		v := test.v
		if got := IndexFunc(test.s, func(x int) bool { return x == v }); got != test.want {
			t.Errorf("IndexFunc(%v, == %v) = %d, want %d", test.s, test.v, got, test.want)
		}
	}
	s1 := []string{"hi", "HI"}
	if got := IndexFunc(s1, func(s string) bool { return s == "HI" }); got != 1 {
		t.Errorf("IndexFunc(%v, == HI) = %d, want %d", s1, got, 1)
	}
}

func TestContains(t *testing.T) {
	for _, test := range indexTests {
		if got := Contains(test.s, test.v); got != (test.want != -1) {
			t.Errorf("Contains(%v, %v) = %t, want %t", test.s, test.v, got, test.want != -1)
		}
	}
}

func TestContainsFunc(t *testing.T) {
	for _, test := range indexTests {
		v := test.v
		if got := ContainsFunc(test.s, func(x int) bool { return x == v }); got != (test.want != -1) {
			t.Errorf("ContainsFunc(%v, == %v) = %t, want %t", test.s, test.v, got, test.want != -1)
		}
	}
}

var insertTests = [...]struct {
	s    []int
	i    int
	add  []int
	want []int
}{
	{[]int{1, 2, 3}, 0, []int{4}, []int{4, 1, 2, 3}},
	{[]int{1, 2, 3}, 1, []int{4}, []int{1, 4, 2, 3}},
	{[]int{1, 2, 3}, 3, []int{4}, []int{1, 2, 3, 4}},
	{[]int{1, 2, 3}, 2, []int{4, 5}, []int{1, 2, 4, 5, 3}},
}

// vecOf returns a new Vec holding the elements of s, with no spare room,
// so that inserting grows it.
func vecOf(s []int) vec.Vec[int] {
	v := vec.New[int]()
	v.Append(s...)
	return v
}

// TestInsert checks vec.Vec's Insert, which replaces Insert.
func TestInsert(t *testing.T) {
	for _, test := range insertTests {
		v := vecOf(test.s)
		v.Insert(test.i, test.add...)
		if !Equal(v.Slice(), test.want) {
			t.Errorf("Insert(%v, %d, %v...) = %v, want %v", test.s, test.i, test.add, v.Slice(), test.want)
		}
		v.Free()
	}
}

func TestInsertAll(t *testing.T) {
	const N = 10
	var a, wbuf [2 * N]int
	for k := range a {
		a[k] = 100 + k
	}
	for n := 0; n <= N; n++ { // length
		for i := 0; i <= n; i++ { // insertion point
			for x := 0; x <= N; x++ { // start of inserted data
				for y := x; y <= N; y++ { // end of inserted data
					v := vec.New[int]()
					for k := 0; k < n; k++ {
						v.Push(k)
					}
					w := 0
					for k := 0; k < i; k++ {
						wbuf[w] = k
						w++
					}
					w += copy(wbuf[w:], a[x:y])
					for k := i; k < n; k++ {
						wbuf[w] = k
						w++
					}
					v.Insert(i, a[x:y]...)
					if !Equal(v.Slice(), wbuf[:w]) {
						t.Errorf("Insert n=%d i=%d x=%d y=%d, got %v want %v", n, i, x, y, v.Slice(), wbuf[:w])
					}
					v.Free()
				}
			}
		}
	}
}

var deleteTests = [...]struct {
	s    []int
	i, j int
	want []int
}{
	{[]int{1, 2, 3}, 0, 0, []int{1, 2, 3}},
	{[]int{1, 2, 3}, 0, 1, []int{2, 3}},
	{[]int{1, 2, 3}, 3, 3, []int{1, 2, 3}},
	{[]int{1, 2, 3}, 0, 2, []int{3}},
	{[]int{1, 2, 3}, 0, 3, []int{}},
}

func TestDelete(t *testing.T) {
	for _, test := range deleteTests {
		var buf [8]int
		c := buf[:copy(buf[:], test.s)]
		if got := Delete(c, test.i, test.j); !Equal(got, test.want) {
			t.Errorf("Delete(%v, %d, %d) = %v, want %v", test.s, test.i, test.j, got, test.want)
		}
	}
}

func isTrue(int) bool   { return true }
func isFalse(int) bool  { return false }
func isGT2(i int) bool  { return i > 2 }
func isLT2(i int) bool  { return i < 2 }
func isGE10(i int) bool { return i >= 10 }

var deleteFuncTests = [...]struct {
	s    []int
	fn   func(int) bool
	want []int
}{
	{nil, isTrue, nil},
	{[]int{1, 2, 3}, isTrue, nil},
	{[]int{1, 2, 3}, isFalse, []int{1, 2, 3}},
	{[]int{1, 2, 3}, isGT2, []int{1, 2}},
	{[]int{1, 2, 3}, isLT2, []int{2, 3}},
	{[]int{10, 2, 30}, isGE10, []int{2}},
}

func TestDeleteFunc(t *testing.T) {
	for i, test := range deleteFuncTests {
		var buf [8]int
		c := buf[:copy(buf[:], test.s)]
		if got := DeleteFunc(c, test.fn); !Equal(got, test.want) {
			t.Errorf("DeleteFunc case %d: got %v, want %v", i, got, test.want)
		}
	}
}

func TestDeleteClearTail(t *testing.T) {
	var a, b, c, d, e, f int
	mem := [...]*int{&a, &b, &c, &d, &e, &f}
	s := mem[0:5] // there is 1 element beyond len(s), within cap(s)

	s = Delete(s, 2, 4)

	if len(s) != 3 {
		t.Errorf("Delete: len = %d, want 3", len(s))
	}
	if mem[3] != nil || mem[4] != nil {
		t.Errorf("Delete: want nil discarded elements, got %v, %v", mem[3], mem[4])
	}
	if mem[5] == nil {
		t.Errorf("Delete: want unchanged elements beyond original len, got nil")
	}
}

func TestDeleteFuncClearTail(t *testing.T) {
	var a, b, c, d, e, f int
	mem := [...]*int{&a, &b, &c, &d, &e, &f}
	*mem[2], *mem[3] = 42, 42
	s := mem[0:5] // there is 1 element beyond len(s), within cap(s)

	s = DeleteFunc(s, func(i *int) bool {
		return i != nil && *i == 42
	})

	if len(s) != 3 {
		t.Errorf("DeleteFunc: len = %d, want 3", len(s))
	}
	if mem[3] != nil || mem[4] != nil {
		t.Errorf("DeleteFunc: want nil discarded elements, got %v, %v", mem[3], mem[4])
	}
	if mem[5] == nil {
		t.Errorf("DeleteFunc: want unchanged elements beyond original len, got nil")
	}
}

func TestClone(t *testing.T) {
	s1 := []int{1, 2, 3}
	v := Clone(s1)
	defer v.Free()
	if !Equal(s1, v.Slice()) {
		t.Errorf("Clone(%v) = %v, want %v", s1, v.Slice(), s1)
	}
	s1[0] = 4
	want := []int{1, 2, 3}
	if !Equal(v.Slice(), want) {
		t.Errorf("Clone(%v) changed unexpectedly to %v", want, v.Slice())
	}
	if v.Cap() != 3 {
		t.Errorf("Clone: Cap = %d, want 3", v.Cap())
	}
	e := Clone(s1[:0])
	if e.Len() != 0 {
		t.Errorf("Clone(empty).Len() = %d, want 0", e.Len())
	}
	e.Free()
}

var compactTests = [...]struct {
	name string
	s    []int
	want []int
}{
	{"nil", nil, nil},
	{"one", []int{1}, []int{1}},
	{"sorted", []int{1, 2, 3}, []int{1, 2, 3}},
	{"1 item", []int{1, 1, 2}, []int{1, 2}},
	{"unsorted", []int{1, 2, 1}, []int{1, 2, 1}},
	{"many", []int{1, 2, 2, 3, 3, 4}, []int{1, 2, 3, 4}},
}

func TestCompact(t *testing.T) {
	for _, test := range compactTests {
		var buf [8]int
		c := buf[:copy(buf[:], test.s)]
		if got := Compact(c); !Equal(got, test.want) {
			t.Errorf("Compact(%s) = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestCompactFunc(t *testing.T) {
	for _, test := range compactTests {
		var buf [8]int
		c := buf[:copy(buf[:], test.s)]
		if got := CompactFunc(c, equal[int]); !Equal(got, test.want) {
			t.Errorf("CompactFunc(%s, equal) = %v, want %v", test.name, got, test.want)
		}
	}

	s1 := [...]int{1, 2, 3, 4, 5, 6}
	want := []int{1, 3, 5}
	if got := CompactFunc(s1[:], offByOneNot); !Equal(got, want) {
		t.Errorf("CompactFunc = %v, want %v", got, want)
	}
	// The tail is zeroed.
	for i := 3; i < 6; i++ {
		if s1[i] != 0 {
			t.Errorf("CompactFunc: element %d not cleared: %d", i, s1[i])
		}
	}
}

// offByOneNot reports whether integers are in the same pair {2k-1, 2k}, so
// that CompactFunc collapses each pair.
func offByOneNot(v1, v2 int) bool {
	return (v1+1)/2 == (v2+1)/2
}

func TestCompactClearTail(t *testing.T) {
	one, two, three, four := 1, 2, 3, 4
	mem := [...]*int{&one, &one, &two, &two, &three, &four}
	s := mem[0:5] // there is 1 element beyond len(s), within cap(s)
	copy := [...]*int{&one, &two, &three}

	s = Compact(s)

	if len(s) != len(copy) {
		t.Fatalf("Compact(%v) = %v, want %v", mem[:5], s, copy)
	}
	for i := range s {
		if s[i] != copy[i] {
			t.Fatalf("Compact: element %d differs", i)
		}
	}
	if mem[3] != nil || mem[4] != nil {
		t.Errorf("Compact: want nil discarded elements, got %v, %v", mem[3], mem[4])
	}
	if mem[5] != &four {
		t.Errorf("Compact: want unchanged elements beyond original len")
	}
}

var replaceTests = [...]struct {
	s    []int
	i, j int
	v    []int
	want []int
}{
	{[]int{1, 2, 3}, 0, 0, nil, []int{1, 2, 3}},
	{[]int{1, 2, 3}, 0, 1, nil, []int{2, 3}},
	{[]int{1, 2, 3}, 0, 1, []int{4}, []int{4, 2, 3}},
	{[]int{1, 2, 3}, 0, 1, []int{4, 5}, []int{4, 5, 2, 3}},
	{[]int{1, 2, 3}, 1, 2, []int{4, 5, 6}, []int{1, 4, 5, 6, 3}},
	{[]int{1, 2, 3}, 2, 3, []int{4, 5, 6}, []int{1, 2, 4, 5, 6}},
	{[]int{1, 2, 3}, 3, 3, []int{4, 5, 6}, []int{1, 2, 3, 4, 5, 6}},
	{[]int{1, 2, 3}, 0, 3, nil, []int{}},
	{[]int{1, 2, 3, 4}, 1, 3, []int{9}, []int{1, 9, 4}},
}

// TestReplace checks vec.Vec's Replace, which replaces Replace.
func TestReplace(t *testing.T) {
	for _, test := range replaceTests {
		v := vecOf(test.s)
		v.Replace(test.i, test.j, test.v...)
		if !Equal(v.Slice(), test.want) {
			t.Errorf("Replace(%v, %d, %d, %v) = %v, want %v", test.s, test.i, test.j, test.v, v.Slice(), test.want)
		}
		v.Free()
	}
}

func TestReplaceAll(t *testing.T) {
	const N = 10
	var a, wbuf [2 * N]int
	for k := range a {
		a[k] = 100 + k
	}
	for n := 0; n <= N; n++ { // length
		for i := 0; i <= n; i++ { // start of the replaced range
			for j := i; j <= n; j++ { // end of the replaced range
				for x := 0; x <= N; x++ { // start of inserted data
					for y := x; y <= N; y++ { // end of inserted data
						v := vec.New[int]()
						for k := 0; k < n; k++ {
							v.Push(k)
						}
						w := 0
						for k := 0; k < i; k++ {
							wbuf[w] = k
							w++
						}
						w += copy(wbuf[w:], a[x:y])
						for k := j; k < n; k++ {
							wbuf[w] = k
							w++
						}
						v.Replace(i, j, a[x:y]...)
						if !Equal(v.Slice(), wbuf[:w]) {
							t.Errorf("Replace n=%d i=%d j=%d x=%d y=%d, got %v want %v", n, i, j, x, y, v.Slice(), wbuf[:w])
						}
						v.Free()
					}
				}
			}
		}
	}
}

func TestClip(t *testing.T) {
	var buf [10]int
	s1 := buf[:3]
	if cap(s1) != 10 {
		t.Fatalf("cap = %d, want 10", cap(s1))
	}
	s2 := Clip(s1)
	if len(s2) != 3 || cap(s2) != 3 {
		t.Errorf("Clip: len %d cap %d, want 3 3", len(s2), cap(s2))
	}
}

func TestReverse(t *testing.T) {
	even := []int{3, 1, 4, 1, 5, 9} // len = 6
	Reverse(even)
	if want := []int{9, 5, 1, 4, 1, 3}; !Equal(even, want) {
		t.Errorf("Reverse(even) = %v, want %v", even, want)
	}

	odd := []int{3, 1, 4, 1, 5, 9, 2} // len = 7
	Reverse(odd)
	if want := []int{2, 9, 5, 1, 4, 1, 3}; !Equal(odd, want) {
		t.Errorf("Reverse(odd) = %v, want %v", odd, want)
	}

	words := []string{"one", "two", "three"}
	Reverse(words)
	if want := []string{"three", "two", "one"}; !Equal(words, want) {
		t.Errorf("Reverse(words) = %v, want %v", words, want)
	}

	// Reverse does not change a nil or empty slice.
	var nilSlice []int
	Reverse(nilSlice)
	Reverse(nilSlice[:0])
}

func TestConcat(t *testing.T) {
	cases := [...]struct {
		s    [][]int
		want []int
	}{
		{[][]int{nil}, []int{}},
		{[][]int{{1}}, []int{1}},
		{[][]int{{1}, {2}}, []int{1, 2}},
		{[][]int{{1}, nil, {2}}, []int{1, 2}},
		{[][]int{{1, 2}, {3, 4, 5}, {}}, []int{1, 2, 3, 4, 5}},
	}
	for _, tc := range cases {
		v := Concat(tc.s...)
		if !Equal(v.Slice(), tc.want) {
			t.Errorf("Concat(%v) = %v, want %v", tc.s, v.Slice(), tc.want)
		}
		if v.Cap() != len(tc.want) {
			t.Errorf("Concat(%v): Cap = %d, want %d", tc.s, v.Cap(), len(tc.want))
		}
		v.Free()
	}
}

func TestRepeat(t *testing.T) {
	cases := [...]struct {
		x     []int
		count int
		want  []int
	}{
		{nil, 0, []int{}},
		{[]int{1}, 0, []int{}},
		{[]int{1}, 1, []int{1}},
		{[]int{1, 2}, 3, []int{1, 2, 1, 2, 1, 2}},
		{[]int{1, 2, 3}, 2, []int{1, 2, 3, 1, 2, 3}},
	}
	for _, tc := range cases {
		v := Repeat(tc.x, tc.count)
		if !Equal(v.Slice(), tc.want) {
			t.Errorf("Repeat(%v, %d) = %v, want %v", tc.x, tc.count, v.Slice(), tc.want)
		}
		v.Free()
	}
}

func TestInAllocator(t *testing.T) {
	// CloneIn, ConcatIn and RepeatIn allocate from the allocator passed in.
	var mem [1024]byte
	a := arena.New(mem[:])
	v := CloneIn(&a, []int{1, 2, 3})
	w := ConcatIn(&a, []int{4}, []int{5, 6})
	x := RepeatIn(&a, []int{7, 8}, 2)
	if !Equal(v.Slice(), []int{1, 2, 3}) || !Equal(w.Slice(), []int{4, 5, 6}) || !Equal(x.Slice(), []int{7, 8, 7, 8}) {
		t.Errorf("got %v %v %v", v.Slice(), w.Slice(), x.Slice())
	}
	if v.Allocator() != alloc.Allocator(&a) {
		t.Errorf("CloneIn did not record the allocator")
	}
	// 3 + 3 + 4 words, with alignment: well inside the arena, and nothing
	// came from anywhere else, so resetting the arena reclaims it all.
	a.Reset()
}

// Sorting, from sort_test.go.

var ints = [...]int{74, 59, 238, -784, 9845, 959, 905, 0, 0, 42, 7586, -5467984, 7586}
var float64s = [...]float64{74.3, 59.0, math.Inf(1), 238.2, -784.0, 2.3, math.Inf(-1), 9845.768, -959.7485, 905, 7.8, 7.8, 74.3, 59.0, math.Inf(1), 238.2, -784.0, 2.3}
var strs = [...]string{"", "Hello", "foo", "bar", "foo", "f00", "%*&^*&^&", "***"}

func TestSortIntSlice(t *testing.T) {
	data := ints
	Sort(data[:])
	if !IsSorted(data[:]) {
		t.Errorf("sorted %v", ints)
		t.Errorf("   got %v", data)
	}
}

func TestSortFuncIntSlice(t *testing.T) {
	data := ints
	SortFunc(data[:], func(a, b int) int { return a - b })
	if !IsSorted(data[:]) {
		t.Errorf("sorted %v", ints)
		t.Errorf("   got %v", data)
	}
}

func TestSortFloat64Slice(t *testing.T) {
	data := float64s
	Sort(data[:])
	if !IsSorted(data[:]) {
		t.Errorf("sorted %v", float64s)
		t.Errorf("   got %v", data)
	}
}

func TestSortStringSlice(t *testing.T) {
	data := strs
	Sort(data[:])
	if !IsSorted(data[:]) {
		t.Errorf("sorted %v", strs)
		t.Errorf("   got %v", data)
	}
}

// lcg is a small deterministic generator for test data.
type lcg struct{ state uint64 }

func (g *lcg) intn(n int) int {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return int(g.state>>33) % n
}

func TestSortLargeRandom(t *testing.T) {
	data := make([]int, 100000)
	g := lcg{state: 7}
	for i := range data {
		data[i] = g.intn(100)
	}
	if IsSorted(data) {
		t.Fatalf("terrible rand")
	}
	Sort(data)
	if !IsSorted(data) {
		t.Errorf("sort didn't sort - 100000 ints")
	}
}

func TestSortPatterns(t *testing.T) {
	for _, n := range [...]int{0, 1, 2, 3, 11, 12, 13, 50, 129, 1000} {
		data := make([]int, n)
		g := lcg{state: uint64(n) + 1}
		for pattern := 0; pattern < 8; pattern++ {
			for i := range data {
				switch pattern {
				case 0:
					data[i] = g.intn(1000)
				case 1:
					data[i] = i
				case 2:
					data[i] = n - i
				case 3:
					data[i] = 7
				case 4:
					data[i] = i % 17
				case 5:
					if i < n/2 {
						data[i] = i
					} else {
						data[i] = n - i
					}
				case 6:
					data[i] = g.intn(4)
				case 7:
					data[i] = (i * 7919) % 101
				}
			}
			SortFunc(data, cmp.Compare[int])
			if !IsSortedFunc(data, cmp.Compare[int]) {
				t.Fatalf("SortFunc did not sort n=%d pattern=%d", n, pattern)
			}
		}
	}
}

type intPair struct {
	a, b int
}

// Pairs compare on a only.
func intPairCmp(x, y intPair) int {
	return x.a - y.a
}

// inOrder checks if a-equal elements were not reordered.
func inOrder(d []intPair) bool {
	lastA, lastB := -1, 0
	for i := 0; i < len(d); i++ {
		if lastA != d[i].a {
			lastA = d[i].a
			lastB = d[i].b
			continue
		}
		if d[i].b <= lastB {
			return false
		}
		lastB = d[i].b
	}
	return true
}

func initB(d []intPair) {
	for i := range d {
		d[i].b = i
	}
}

func TestStability(t *testing.T) {
	n, m := 10000, 100
	data := make([]intPair, n)
	g := lcg{state: 3}

	// random distribution
	for i := 0; i < len(data); i++ {
		data[i].a = g.intn(m)
	}
	if IsSortedFunc(data, intPairCmp) {
		t.Fatalf("terrible rand")
	}
	initB(data)
	SortStableFunc(data, intPairCmp)
	if !IsSortedFunc(data, intPairCmp) {
		t.Errorf("Stable didn't sort %d ints", n)
	}
	if !inOrder(data) {
		t.Errorf("Stable wasn't stable on %d ints", n)
	}

	// already sorted
	initB(data)
	SortStableFunc(data, intPairCmp)
	if !IsSortedFunc(data, intPairCmp) {
		t.Errorf("Stable shuffled sorted %d ints (order)", n)
	}
	if !inOrder(data) {
		t.Errorf("Stable shuffled sorted %d ints (stability)", n)
	}

	// sorted reversed
	for i := 0; i < len(data); i++ {
		data[i].a = len(data) - i
	}
	initB(data)
	SortStableFunc(data, intPairCmp)
	if !IsSortedFunc(data, intPairCmp) {
		t.Errorf("Stable didn't sort %d ints", n)
	}
	if !inOrder(data) {
		t.Errorf("Stable wasn't stable on %d ints", n)
	}
}

type S struct {
	a int
	b string
}

func cmpS(s1, s2 S) int {
	return cmp.Compare(s1.a, s2.a)
}

var minMaxTests = [...]struct {
	data    []int
	wantMin int
	wantMax int
}{
	{[]int{7}, 7, 7},
	{[]int{1, 2}, 1, 2},
	{[]int{2, 1}, 1, 2},
	{[]int{1, 2, 3}, 1, 3},
	{[]int{3, 2, 1}, 1, 3},
	{[]int{2, 1, 3}, 1, 3},
	{[]int{2, 2, 3}, 2, 3},
	{[]int{3, 2, 3}, 2, 3},
	{[]int{0, 2, -9}, -9, 2},
}

func intCmp(a, b int) int { return a - b }

func TestMinMax(t *testing.T) {
	for _, tt := range minMaxTests {
		if got := Min(tt.data); got != tt.wantMin {
			t.Errorf("Min(%v) = %v, want %v", tt.data, got, tt.wantMin)
		}
		if got := MinFunc(tt.data, intCmp); got != tt.wantMin {
			t.Errorf("MinFunc(%v) = %v, want %v", tt.data, got, tt.wantMin)
		}
		if got := Max(tt.data); got != tt.wantMax {
			t.Errorf("Max(%v) = %v, want %v", tt.data, got, tt.wantMax)
		}
		if got := MaxFunc(tt.data, intCmp); got != tt.wantMax {
			t.Errorf("MaxFunc(%v) = %v, want %v", tt.data, got, tt.wantMax)
		}
	}

	svals := []S{
		{1, "a"},
		{2, "a"},
		{1, "b"},
		{2, "b"},
	}

	gotMin := MinFunc(svals, cmpS)
	wantMin := S{1, "a"}
	if gotMin != wantMin {
		t.Errorf("MinFunc(%v) = %v, want %v", svals, gotMin, wantMin)
	}

	gotMax := MaxFunc(svals, cmpS)
	wantMax := S{2, "a"}
	if gotMax != wantMax {
		t.Errorf("MaxFunc(%v) = %v, want %v", svals, gotMax, wantMax)
	}
}

func TestMinMaxNaNs(t *testing.T) {
	fs := [...]float64{1.0, 999.9, 3.14, -400.4, -5.14}
	if Min(fs[:]) != -400.4 {
		t.Errorf("got min %v, want -400.4", Min(fs[:]))
	}
	if Max(fs[:]) != 999.9 {
		t.Errorf("got max %v, want 999.9", Max(fs[:]))
	}

	// No matter which element of fs is replaced with a NaN, both Min and Max
	// should propagate the NaN to their output.
	for i := 0; i < len(fs); i++ {
		testfs := fs
		testfs[i] = math.NaN()

		fmin := Min(testfs[:])
		if !math.IsNaN(fmin) {
			t.Errorf("got min %v, want NaN", fmin)
		}

		fmax := Max(testfs[:])
		if !math.IsNaN(fmax) {
			t.Errorf("got max %v, want NaN", fmax)
		}
	}
}

func cmpStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func TestBinarySearch(t *testing.T) {
	str1 := []string{"foo"}
	str2 := []string{"ab", "ca"}
	str3 := []string{"mo", "qo", "vo"}
	str4 := []string{"ab", "ad", "ca", "xy"}

	// slice with repeating elements
	strRepeats := []string{"ba", "ca", "da", "da", "da", "ka", "ma", "ma", "ta"}

	// slice with all element equal
	strSame := []string{"xx", "xx", "xx"}

	tests := []struct {
		data      []string
		target    string
		wantPos   int
		wantFound bool
	}{
		{[]string{}, "foo", 0, false},
		{[]string{}, "", 0, false},

		{str1, "foo", 0, true},
		{str1, "bar", 0, false},
		{str1, "zx", 1, false},

		{str2, "aa", 0, false},
		{str2, "ab", 0, true},
		{str2, "ad", 1, false},
		{str2, "ca", 1, true},
		{str2, "ra", 2, false},

		{str3, "bb", 0, false},
		{str3, "mo", 0, true},
		{str3, "nb", 1, false},
		{str3, "qo", 1, true},
		{str3, "tr", 2, false},
		{str3, "vo", 2, true},
		{str3, "xr", 3, false},

		{str4, "aa", 0, false},
		{str4, "ab", 0, true},
		{str4, "ac", 1, false},
		{str4, "ad", 1, true},
		{str4, "ax", 2, false},
		{str4, "ca", 2, true},
		{str4, "cc", 3, false},
		{str4, "dd", 3, false},
		{str4, "xy", 3, true},
		{str4, "zz", 4, false},

		{strRepeats, "da", 2, true},
		{strRepeats, "db", 5, false},
		{strRepeats, "ma", 6, true},
		{strRepeats, "mb", 8, false},

		{strSame, "xx", 0, true},
		{strSame, "ab", 0, false},
		{strSame, "zz", 3, false},
	}
	for _, tt := range tests {
		pos, found := BinarySearch(tt.data, tt.target)
		if pos != tt.wantPos || found != tt.wantFound {
			t.Errorf("BinarySearch(%q) got (%v, %v), want (%v, %v)", tt.target, pos, found, tt.wantPos, tt.wantFound)
		}
		pos, found = BinarySearchFunc(tt.data, tt.target, cmpStr)
		if pos != tt.wantPos || found != tt.wantFound {
			t.Errorf("BinarySearchFunc(%q) got (%v, %v), want (%v, %v)", tt.target, pos, found, tt.wantPos, tt.wantFound)
		}
	}
}

func TestBinarySearchInts(t *testing.T) {
	data := []int{20, 30, 40, 50, 60, 70, 80, 90}
	tests := [...]struct {
		target    int
		wantPos   int
		wantFound bool
	}{
		{20, 0, true},
		{23, 1, false},
		{43, 3, false},
		{80, 6, true},
	}
	for _, tt := range tests {
		pos, found := BinarySearch(data, tt.target)
		if pos != tt.wantPos || found != tt.wantFound {
			t.Errorf("BinarySearch(%d) got (%v, %v), want (%v, %v)", tt.target, pos, found, tt.wantPos, tt.wantFound)
		}
		pos, found = BinarySearchFunc(data, tt.target, intCmp)
		if pos != tt.wantPos || found != tt.wantFound {
			t.Errorf("BinarySearchFunc(%d) got (%v, %v), want (%v, %v)", tt.target, pos, found, tt.wantPos, tt.wantFound)
		}
	}
}

func TestBinarySearchFloats(t *testing.T) {
	data := []float64{math.NaN(), -0.25, 0.0, 1.4}
	tests := [...]struct {
		target    float64
		wantPos   int
		wantFound bool
	}{
		{math.NaN(), 0, true},
		{math.Inf(-1), 1, false},
		{-0.25, 1, true},
		{0.0, 2, true},
		{1.4, 3, true},
		{1.5, 4, false},
	}
	for _, tt := range tests {
		pos, found := BinarySearch(data, tt.target)
		if pos != tt.wantPos || found != tt.wantFound {
			t.Errorf("BinarySearch(%v) got (%v, %v), want (%v, %v)", tt.target, pos, found, tt.wantPos, tt.wantFound)
		}
	}
}

func BenchmarkSortInts(b *testing.B) {
	var data [1000]int
	g := lcg{state: 1}
	for i := 0; i < b.N; i++ {
		for j := range data {
			data[j] = g.intn(1 << 20)
		}
		Sort(data[:])
	}
}

func BenchmarkSortFuncInts(b *testing.B) {
	var data [1000]int
	g := lcg{state: 1}
	for i := 0; i < b.N; i++ {
		for j := range data {
			data[j] = g.intn(1 << 20)
		}
		SortFunc(data[:], cmp.Compare[int])
	}
}
