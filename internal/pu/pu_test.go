package pu

import (
	"fmt"
	"slices"
	"testing"
)

func TestVec(t *testing.T) {
	var v Vec[int] // the zero Vec is ready to use
	for i := range 5 {
		v.Push(i)
	}
	v.Append(5, 6)
	if v.Len() != 7 || v.At(6) != 6 {
		t.Fatalf("Push/Append: %v", v.Slice())
	}
	if x, ok := v.Pop(); !ok || x != 6 {
		t.Errorf("Pop = %d, %v", x, ok)
	}
	v.Insert(0, -1)
	if got := v.Remove(1); got != 0 {
		t.Errorf("Remove = %d", got)
	}
	if got := v.SwapRemove(0); got != -1 || v.At(0) != 5 {
		t.Errorf("SwapRemove = %d, then %v", got, v.Slice())
	}
	*v.Ptr(1) = 10
	v.Set(2, 20)
	if want := []int{5, 10, 20, 3, 4}; !slices.Equal(v.Slice(), want) {
		t.Errorf("elements %v, want %v", v.Slice(), want)
	}
	if s := v.Slice(); cap(s) != len(s) {
		t.Error("Slice has spare capacity")
	}
	c := v.Clone()
	c.Set(0, 99)
	if v.At(0) != 5 {
		t.Error("Clone shares storage")
	}
	v.Reserve(10)
	if v.Cap() < v.Len()+10 {
		t.Error("Reserve")
	}
	if last, ok := v.Last(); !ok || last != 4 {
		t.Error("Last")
	}
	v.Truncate(1)
	v.Clear()
	if _, ok := v.Last(); ok || v.Len() != 0 {
		t.Error("Clear")
	}
	if _, ok := v.Pop(); ok {
		t.Error("Pop of empty")
	}
	w := VecWithCapacity[string](3)
	if w.Cap() < 3 || w.Len() != 0 {
		t.Error("VecWithCapacity")
	}
	c.Drop()
	if c.Len() != 0 {
		t.Error("Drop")
	}
	for _, f := range []func(){
		func() { v.Insert(5, 1) },
		func() { v.Truncate(-1) },
		func() { v.Reserve(-1) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			f()
		}()
	}
}

func TestMap(t *testing.T) {
	var m Map[string, int] // the zero Map is ready to use
	if m.Ptr("a") != nil || m.At("a") != 0 || m.Has("a") {
		t.Error("reads of the zero Map")
	}
	m.Set("a", 1)
	p, inserted := m.Upsert("b")
	if !inserted {
		t.Error("Upsert of an absent key")
	}
	*p = 2
	if q, again := m.Upsert("b"); again || q != p {
		t.Error("Upsert of a present key")
	}
	*m.Ptr("a") += 10
	if v, ok := m.Get("a"); !ok || v != 11 || m.At("b") != 2 || m.Len() != 2 {
		t.Errorf("Get a = %d, %v; b = %d", v, ok, m.At("b"))
	}
	c := m.Clone()
	c.Set("a", 0)
	if m.At("a") != 11 {
		t.Error("Clone shares values")
	}
	it := m.Iter()
	sum := 0
	for _, v, ok := it.Next(); ok; _, v, ok = it.Next() {
		sum += v
		*it.Value() *= 2
	}
	if sum != 13 || m.At("a") != 22 || m.At("b") != 4 {
		t.Errorf("iteration: sum %d, a %d, b %d", sum, m.At("a"), m.At("b"))
	}
	if !m.Delete("a") || m.Delete("a") || m.Len() != 1 {
		t.Error("Delete")
	}
	n := NewMap[int, string](4)
	n.Set(1, "x")
	n.Clear()
	if n.Len() != 0 {
		t.Error("Clear")
	}
	m.Drop()
	if m.Len() != 0 {
		t.Error("Drop")
	}
	var z Map[int, int]
	if p, ins := z.Upsert(3); !ins || *p != 0 {
		t.Error("Upsert on the zero Map")
	}
}

func TestSet(t *testing.T) {
	var s Set[int]
	if !s.Add(1) || s.Add(1) || !s.Add(2) || !s.Has(1) || s.Len() != 2 {
		t.Error("Add/Has")
	}
	c := s.Clone()
	c.Add(3)
	if s.Has(3) || c.Len() != 3 {
		t.Error("Clone")
	}
	var got []int
	it := c.Iter()
	for k, ok := it.Next(); ok; k, ok = it.Next() {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("Iter: %v", got)
	}
	if !s.Delete(1) || s.Delete(1) {
		t.Error("Delete")
	}
	n := NewSet[string](2)
	n.Add("x")
	n.Clear()
	s.Drop()
	if n.Len() != 0 || s.Len() != 0 {
		t.Error("Clear/Drop")
	}
}

func TestBufAndNew(t *testing.T) {
	var b Buf
	b.Grow(8)
	b.WriteString("a")
	b.WriteByte('b')
	b.WriteRune('é')
	b.Write([]byte("c"))
	_, _ = fmt.Fprintf(&b, "%d", 42)
	if b.String() != "abéc42" || b.Len() != 7 || string(b.Bytes()) != b.String() || b.Cap() < 7 {
		t.Errorf("Buf = %q", b.String())
	}
	b.Reset()
	if b.Len() != 0 {
		t.Error("Reset")
	}
	s := Sprintf("%s-%d", "x", 7)
	if s.String() != "x-7" {
		t.Errorf("Sprintf = %q", s.String())
	}
	s.Drop()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Grow(-1) did not panic")
			}
		}()
		b.Grow(-1)
	}()
	type node struct{ next *node }
	a := New(node{})
	c := New(node{next: a})
	if c.next != a || a == c {
		t.Error("New")
	}
}
