// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's container/heap/heap_test.go. A slice cannot grow, so the
// heaps over slices start with the capacity they need; the maps and
// math/rand of Go's tests are an array and a small generator.

package heap

import "pluto/testing"

type myHeap []int

func (h *myHeap) Less(i, j int) bool {
	return (*h)[i] < (*h)[j]
}

func (h *myHeap) Swap(i, j int) {
	(*h)[i], (*h)[j] = (*h)[j], (*h)[i]
}

func (h *myHeap) Len() int {
	return len(*h)
}

func (h *myHeap) Pop() (v any) {
	*h, v = (*h)[:h.Len()-1], (*h)[h.Len()-1]
	return
}

// Push stores v in the slice's spare capacity: a heap over a slice is
// as big as the array it views.
func (h *myHeap) Push(v any) {
	n := len(*h)
	*h = (*h)[:n+1]
	(*h)[n] = v.(int)
}

func (h myHeap) verify(t *testing.T, i int) {
	t.Helper()
	n := h.Len()
	j1 := 2*i + 1
	j2 := 2*i + 2
	if j1 < n {
		if h.Less(j1, i) {
			t.Errorf("heap invariant invalidated [%d] = %d > [%d] = %d", i, h[i], j1, h[j1])
			return
		}
		h.verify(t, j1)
	}
	if j2 < n {
		if h.Less(j2, i) {
			t.Errorf("heap invariant invalidated [%d] = %d > [%d] = %d", i, h[i], j1, h[j2])
			return
		}
		h.verify(t, j2)
	}
}

func TestInit0(t *testing.T) {
	var buf [64]int
	h := myHeap(buf[:0])
	for i := 20; i > 0; i-- {
		h.Push(0) // all elements are the same
	}
	Init(&h)
	h.verify(t, 0)

	for i := 1; h.Len() > 0; i++ {
		x := Pop(&h).(int)
		h.verify(t, 0)
		if x != 0 {
			t.Errorf("%d.th pop got %d; want %d", i, x, 0)
		}
	}
}

func TestInit1(t *testing.T) {
	var buf [64]int
	h := myHeap(buf[:0])
	for i := 20; i > 0; i-- {
		h.Push(i) // all elements are different
	}
	Init(&h)
	h.verify(t, 0)

	for i := 1; h.Len() > 0; i++ {
		x := Pop(&h).(int)
		h.verify(t, 0)
		if x != i {
			t.Errorf("%d.th pop got %d; want %d", i, x, i)
		}
	}
}

func Test(t *testing.T) {
	var buf [64]int
	h := myHeap(buf[:0])
	h.verify(t, 0)

	for i := 20; i > 10; i-- {
		h.Push(i)
	}
	Init(&h)
	h.verify(t, 0)

	for i := 10; i > 0; i-- {
		Push(&h, i)
		h.verify(t, 0)
	}

	for i := 1; h.Len() > 0; i++ {
		x := Pop(&h).(int)
		if i < 20 {
			Push(&h, 20+i)
		}
		h.verify(t, 0)
		if x != i {
			t.Errorf("%d.th pop got %d; want %d", i, x, i)
		}
	}
}

func TestRemove0(t *testing.T) {
	var buf [64]int
	h := myHeap(buf[:0])
	for i := 0; i < 10; i++ {
		h.Push(i)
	}
	h.verify(t, 0)

	for h.Len() > 0 {
		i := h.Len() - 1
		x := Remove(&h, i).(int)
		if x != i {
			t.Errorf("Remove(%d) got %d; want %d", i, x, i)
		}
		h.verify(t, 0)
	}
}

func TestRemove1(t *testing.T) {
	var buf [64]int
	h := myHeap(buf[:0])
	for i := 0; i < 10; i++ {
		h.Push(i)
	}
	h.verify(t, 0)

	for i := 0; h.Len() > 0; i++ {
		x := Remove(&h, 0).(int)
		if x != i {
			t.Errorf("Remove(0) got %d; want %d", x, i)
		}
		h.verify(t, 0)
	}
}

func TestRemove2(t *testing.T) {
	const N = 10

	var buf [64]int
	h := myHeap(buf[:0])
	for i := 0; i < N; i++ {
		h.Push(i)
	}
	h.verify(t, 0)

	var seen [N]bool
	count := 0
	for h.Len() > 0 {
		x := Remove(&h, (h.Len()-1)/2).(int)
		if !seen[x] {
			count++
		}
		seen[x] = true
		h.verify(t, 0)
	}

	if count != N {
		t.Errorf("count = %d; want %d", count, N)
	}
	for i := 0; i < N; i++ {
		if !seen[i] {
			t.Errorf("%d doesn't exist", i)
		}
	}
}

func BenchmarkDup(b *testing.B) {
	const n = 10000
	var buf [n]int
	h := myHeap(buf[:0])
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			Push(&h, 0) // all elements are the same
		}
		for h.Len() > 0 {
			Pop(&h)
		}
	}
}

// lcg is a small deterministic generator for test data.
type lcg struct{ state uint64 }

func (g *lcg) intn(n int) int {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return int(g.state>>33) % n
}

func TestFix(t *testing.T) {
	var buf [64]int
	h := myHeap(buf[:0])
	h.verify(t, 0)

	for i := 200; i > 0; i -= 10 {
		Push(&h, i)
	}
	h.verify(t, 0)

	if h[0] != 10 {
		t.Fatalf("Expected head to be 10, was %d", h[0])
	}
	h[0] = 210
	Fix(&h, 0)
	h.verify(t, 0)

	g := lcg{state: 1}
	for i := 100; i > 0; i-- {
		elem := g.intn(h.Len())
		if i&1 == 0 {
			h[elem] *= 2
		} else {
			h[elem] /= 2
		}
		Fix(&h, elem)
		h.verify(t, 0)
	}
}

// The generic Heap.

type task struct {
	priority int
	name     string
}

func taskLess(a, b task) bool { return a.priority < b.priority }

func verifyHeap(t *testing.T, h *Heap[task]) {
	t.Helper()
	s := h.Slice()
	for i := 1; i < len(s); i++ {
		if taskLess(s[i], s[(i-1)/2]) {
			t.Errorf("heap invariant invalidated: [%d] = %v < parent [%d] = %v", i, s[i], (i-1)/2, s[(i-1)/2])
			return
		}
	}
}

func TestHeapPushPop(t *testing.T) {
	h := New(taskLess)
	defer h.Free()
	if _, ok := h.Pop(); ok {
		t.Errorf("Pop of an empty heap succeeded")
	}
	if _, ok := h.Peek(); ok {
		t.Errorf("Peek of an empty heap succeeded")
	}
	g := lcg{state: 5}
	for i := 0; i < 500; i++ {
		h.Push(task{g.intn(100), "t"})
		verifyHeap(t, &h)
	}
	if h.Len() != 500 {
		t.Fatalf("Len = %d, want 500", h.Len())
	}
	prev := -1
	for h.Len() > 0 {
		top, _ := h.Peek()
		x, ok := h.Pop()
		if !ok || x != top {
			t.Fatalf("Pop = %v, %t; Peek was %v", x, ok, top)
		}
		if x.priority < prev {
			t.Fatalf("Pop out of order: %d after %d", x.priority, prev)
		}
		prev = x.priority
		verifyHeap(t, &h)
	}
}

func TestHeapInit(t *testing.T) {
	h := New(taskLess)
	defer h.Free()
	for i := 20; i > 0; i-- {
		h.AppendUnordered(task{i, "u"})
	}
	h.Init()
	verifyHeap(t, &h)
	for i := 1; h.Len() > 0; i++ {
		x, _ := h.Pop()
		if x.priority != i {
			t.Errorf("%d.th pop got %d; want %d", i, x.priority, i)
		}
	}
}

func TestHeapRemoveFix(t *testing.T) {
	h := New(taskLess)
	defer h.Free()
	for i := 0; i < 10; i++ {
		h.Push(task{i, "r"})
	}
	x := h.Remove(h.Len() - 1)
	verifyHeap(t, &h)
	if h.Len() != 9 {
		t.Fatalf("Len = %d, want 9", h.Len())
	}
	_ = x
	seen := 0
	for h.Len() > 0 {
		x := h.Remove((h.Len() - 1) / 2)
		seen += x.priority
		verifyHeap(t, &h)
	}
	if seen != 0+1+2+3+4+5+6+7+8+9-x.priority {
		t.Errorf("sum of removed priorities = %d", seen)
	}

	g := lcg{state: 9}
	for i := 0; i < 100; i++ {
		h.Push(task{g.intn(1000), "f"})
	}
	for i := 0; i < 200; i++ {
		j := g.intn(h.Len())
		h.Set(j, task{g.intn(1000), "g"})
		verifyHeap(t, &h)
	}
}

func BenchmarkHeapDup(b *testing.B) {
	const n = 10000
	h := New(taskLess)
	defer h.Free()
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			h.Push(task{})
		}
		for h.Len() > 0 {
			h.Pop()
		}
	}
}
