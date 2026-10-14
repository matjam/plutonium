// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's slices/iter_test.go. Collect, Sorted and the like return
// a vec.Vec, and vec.Vec's AppendSeq replaces AppendSeq.

package slices

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/testing"
	"pluto/vec"
)

func TestAll(t *testing.T) {
	for size := 0; size < 10; size++ {
		var buf [10]int
		s := buf[:size]
		for i := range size {
			s[i] = i
		}
		ei, ev := 0, 0
		cnt := 0
		for i, v := range All(s) {
			if i != ei || v != ev {
				t.Errorf("at iteration %d got %d, %d want %d, %d", cnt, i, v, ei, ev)
			}
			ei++
			ev++
			cnt++
		}
		if cnt != size {
			t.Errorf("read %d values expected %d", cnt, size)
		}
	}
}

func TestBackward(t *testing.T) {
	for size := 0; size < 10; size++ {
		var buf [10]int
		s := buf[:size]
		for i := range size {
			s[i] = i
		}
		ei, ev := size-1, size-1
		cnt := 0
		for i, v := range Backward(s) {
			if i != ei || v != ev {
				t.Errorf("at iteration %d got %d, %d want %d, %d", cnt, i, v, ei, ev)
			}
			ei--
			ev--
			cnt++
		}
		if cnt != size {
			t.Errorf("read %d values expected %d", cnt, size)
		}
	}
}

func TestValues(t *testing.T) {
	for size := 0; size < 10; size++ {
		var buf [10]int
		s := buf[:size]
		for i := range size {
			s[i] = i
		}
		ev := 0
		cnt := 0
		for v := range Values(s) {
			if v != ev {
				t.Errorf("at iteration %d got %d want %d", cnt, v, ev)
			}
			ev++
			cnt++
		}
		if cnt != size {
			t.Errorf("read %d values expected %d", cnt, size)
		}
	}
}

func TestIterBreak(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	n := 0
	for range All(s) {
		n++
		if n == 3 {
			break
		}
	}
	for range Backward(s) {
		n++
		if n == 5 {
			break
		}
	}
	for range Values(s) {
		n++
		break
	}
	if n != 6 {
		t.Errorf("n = %d, want 6", n)
	}
}

func testSeq(yield func(int) bool) {
	for i := 0; i < 10; i += 2 {
		if !yield(i) {
			return
		}
	}
}

var testSeqResult = [...]int{0, 2, 4, 6, 8}

func TestAppendSeq(t *testing.T) {
	v := vec.New[int]()
	defer v.Free()
	v.Append(1, 2)
	v.AppendSeq(testSeq)
	want := [...]int{1, 2, 0, 2, 4, 6, 8}
	if !Equal(v.Slice(), want[:]) {
		t.Errorf("got %v, want %v", v.Slice(), want)
	}
}

func TestCollect(t *testing.T) {
	v := Collect(testSeq)
	defer v.Free()
	if !Equal(v.Slice(), testSeqResult[:]) {
		t.Errorf("got %v, want %v", v.Slice(), testSeqResult)
	}
	var empty vec.Vec[int] = Collect(Values([]int(nil)))
	if empty.Len() != 0 {
		t.Errorf("Collect of nothing has %d elements", empty.Len())
	}
}

var iterTests = [...][]string{
	nil,
	{"a"},
	{"a", "b"},
	{"b", "a"},
	strs[:],
}

func TestValuesAppendSeq(t *testing.T) {
	for _, prefix := range iterTests {
		for _, s := range iterTests {
			got := Clone(prefix)
			got.AppendSeq(Values(s))
			want := Concat(prefix, s)
			if !Equal(got.Slice(), want.Slice()) {
				t.Errorf("AppendSeq(%v, Values(%v)) == %v, want %v", prefix, s, got.Slice(), want.Slice())
			}
			want.Free()
			got.Free()
		}
	}
}

func TestValuesCollect(t *testing.T) {
	for _, s := range iterTests {
		got := Collect(Values(s))
		if !Equal(got.Slice(), s) {
			t.Errorf("Collect(Values(%v)) == %v, want %v", s, got.Slice(), s)
		}
		got.Free()
	}
}

func TestSorted(t *testing.T) {
	s := Sorted(Values(ints[:]))
	defer s.Free()
	if !IsSorted(s.Slice()) || s.Len() != len(ints) {
		t.Errorf("sorted %v", ints)
		t.Errorf("   got %v", s.Slice())
	}
}

func TestSortedFunc(t *testing.T) {
	s := SortedFunc(Values(ints[:]), func(a, b int) int { return a - b })
	defer s.Free()
	if !IsSorted(s.Slice()) || s.Len() != len(ints) {
		t.Errorf("sorted %v", ints)
		t.Errorf("   got %v", s.Slice())
	}
}

func TestSortedStableFunc(t *testing.T) {
	n, m := 1000, 100
	dv := vec.Make[intPair](n)
	defer dv.Free()
	data := dv.Slice()
	g := lcg{state: 11}
	for i := range data {
		data[i].a = g.intn(m)
	}
	initB(data)

	s := SortedStableFunc(Values(data), intPairCmp)
	defer s.Free()
	if !IsSortedFunc(s.Slice(), intPairCmp) {
		t.Errorf("SortedStableFunc didn't sort %d ints", n)
	}
	if !inOrder(s.Slice()) {
		t.Errorf("SortedStableFunc wasn't stable on %d ints", n)
	}
}

func TestSortedIn(t *testing.T) {
	// The Vecs allocate from the arena passed in.
	a := arena.NewFixed(4096)
	s := SortedIn(&a, Values([]int{5, 3, 9, 1, 7}))
	if want := [...]int{1, 3, 5, 7, 9}; !Equal(s.Slice(), want[:]) {
		t.Errorf("SortedIn = %v, want %v", s.Slice(), want)
	}
	f := SortedFuncIn(&a, Values([]int{5, 3, 9, 1, 7}), func(a, b int) int { return b - a })
	if want := [...]int{9, 7, 5, 3, 1}; !Equal(f.Slice(), want[:]) {
		t.Errorf("SortedFuncIn = %v, want %v", f.Slice(), want)
	}
	c := CollectIn(&a, Values([]int{2, 4}))
	if want := [...]int{2, 4}; !Equal(c.Slice(), want[:]) {
		t.Errorf("CollectIn = %v, want %v", c.Slice(), want)
	}
	st := SortedStableFuncIn(&a, Values([]int{2, 1}), func(a, b int) int { return a - b })
	if want := [...]int{1, 2}; !Equal(st.Slice(), want[:]) {
		t.Errorf("SortedStableFuncIn = %v, want %v", st.Slice(), want)
	}
	if s.Allocator() != alloc.Allocator(&a) {
		t.Errorf("SortedIn did not record the allocator")
	}
}

func TestChunk(t *testing.T) {
	cases := [...]struct {
		name   string
		s      []int
		n      int
		chunks [][]int
	}{
		{"nil", nil, 1, nil},
		{"empty", []int{}, 1, nil},
		{"short", []int{1, 2}, 3, [][]int{{1, 2}}},
		{"one", []int{1, 2}, 2, [][]int{{1, 2}}},
		{"even", []int{1, 2, 3, 4}, 2, [][]int{{1, 2}, {3, 4}}},
		{"odd", []int{1, 2, 3, 4, 5}, 2, [][]int{{1, 2}, {3, 4}, {5}}},
	}

	for _, tc := range cases {
		var buf [4][]int
		n := 0
		for c := range Chunk(tc.s, tc.n) {
			buf[n] = c
			n++
		}
		chunks := buf[:n]

		if !chunkEqual(chunks, tc.chunks) {
			t.Errorf("%s: Chunk(%v, %d) = %v, want %v", tc.name, tc.s, tc.n, chunks, tc.chunks)
		}

		// Every chunk is clipped: it has no capacity beyond its length, so
		// appending to a chunk cannot clobber the next one.
		for _, c := range chunks {
			if cap(c) != len(c) {
				t.Errorf("%s: chunk %v has capacity %d", tc.name, c, cap(c))
			}
		}
	}
}

func TestChunkRange(t *testing.T) {
	// Verify Chunk iteration can be stopped.
	var buf [4][]int
	n := 0
	for c := range Chunk([]int{1, 2, 3, 4, -100}, 2) {
		if n == 2 {
			// Found enough values, break early.
			break
		}

		buf[n] = c
		n++
	}
	got := buf[:n]

	if want := [][]int{{1, 2}, {3, 4}}; !chunkEqual(got, want) {
		t.Errorf("Chunk iteration did not stop, got %v, want %v", got, want)
	}
}

func chunkEqual(s1, s2 [][]int) bool {
	return EqualFunc(s1, s2, Equal[[]int])
}
