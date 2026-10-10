package sort

import "pluto/testing"

// lcg is a small deterministic generator for test data.
type lcg struct{ state uint64 }

func (g *lcg) next() int {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return int(g.state >> 33)
}

func TestSortInts(t *testing.T) {
	var buf [1000]int
	g := lcg{state: 1}
	for _, n := range [...]int{0, 1, 2, 3, 12, 13, 50, 1000} {
		x := buf[:n]
		for i := range x {
			x[i] = g.next() % 100
		}
		Ints(x)
		if !IntsAreSorted(x) {
			t.Fatalf("Ints did not sort %d elements", n)
		}
	}
}

func TestSortPatterns(t *testing.T) {
	var buf [500]int
	x := buf[:]
	t.Run("ascending", func(t *testing.T) {
		for i := range x {
			x[i] = i
		}
		Sort(x)
		checkSorted(t, x)
	})
	t.Run("descending", func(t *testing.T) {
		for i := range x {
			x[i] = len(x) - i
		}
		Sort(x)
		checkSorted(t, x)
	})
	t.Run("equal", func(t *testing.T) {
		for i := range x {
			x[i] = 7
		}
		Sort(x)
		checkSorted(t, x)
	})
}

func checkSorted(t *testing.T, x []int) {
	t.Helper()
	for i := 1; i < len(x); i++ {
		if x[i-1] > x[i] {
			t.Fatalf("x[%d] = %d > x[%d] = %d", i-1, x[i-1], i, x[i])
		}
	}
}

type pair struct{ key, idx int }

func TestSortStableFunc(t *testing.T) {
	var x [300]pair
	g := lcg{state: 42}
	for i := range x {
		x[i] = pair{key: g.next() % 10, idx: i}
	}
	SortStableFunc(x[:], func(a, b pair) bool { return a.key < b.key })
	for i := 1; i < len(x); i++ {
		a, b := x[i-1], x[i]
		if a.key > b.key || a.key == b.key && a.idx > b.idx {
			t.Fatalf("not stable at %d: %d/%d before %d/%d", i, a.key, a.idx, b.key, b.idx)
		}
	}
}

func TestSortStrings(t *testing.T) {
	x := [...]string{"pluto", "go", "c", "rust", "zig", "ada"}
	Strings(x[:])
	want := [...]string{"ada", "c", "go", "pluto", "rust", "zig"}
	if x != want {
		t.Errorf("Strings gave %v %v %v %v %v %v", x[0], x[1], x[2], x[3], x[4], x[5])
	}
}

func TestBinarySearch(t *testing.T) {
	x := [...]int{1, 3, 5, 7, 9}
	tests := [...]struct {
		target int
		pos    int
		found  bool
	}{
		{0, 0, false},
		{1, 0, true},
		{4, 2, false},
		{9, 4, true},
		{10, 5, false},
	}
	for _, tt := range tests {
		pos, found := BinarySearch(x[:], tt.target)
		if pos != tt.pos || found != tt.found {
			t.Errorf("BinarySearch(%d) = %d, %t, want %d, %t", tt.target, pos, found, tt.pos, tt.found)
		}
	}
	if got := SearchInts(x[:], 6); got != 3 {
		t.Errorf("SearchInts(6) = %d, want 3", got)
	}
	if got := Search(100, func(i int) bool { return i*i >= 50 }); got != 8 {
		t.Errorf("Search = %d, want 8", got)
	}
}

func TestReverse(t *testing.T) {
	x := [...]int{1, 2, 3, 4}
	Reverse(x[:])
	if x != [...]int{4, 3, 2, 1} {
		t.Errorf("Reverse gave %v %v %v %v", x[0], x[1], x[2], x[3])
	}
}

func BenchmarkSortInts(b *testing.B) {
	var buf, src [1000]int
	g := lcg{state: 7}
	for i := range src {
		src[i] = g.next()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		buf = src
		b.StartTimer()
		Ints(buf[:])
	}
}
