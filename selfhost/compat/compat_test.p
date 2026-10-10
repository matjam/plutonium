package compat

import (
	"pluto/hash"
	"pluto/testing"
)

type node struct{ id int }

type named interface{ name() string }

func (n *node) name() string { return "node" }

func TestMapPointerKeys(t *testing.T) {
	m := NewMap[*node, int](0)
	a, b := &node{1}, &node{2}
	m.Set(a, 10)
	*MapSlot(m, b) += 5
	*MapSlot(m, b) += 5
	if MapAt(m, a) != 10 || MapAt(m, b) != 10 || MapLen(m) != 2 {
		t.Fatalf("pointer keys: %d %d %d", MapAt(m, a), MapAt(m, b), MapLen(m))
	}
	if _, ok := MapGet(m, &node{3}); ok {
		t.Fatalf("found a key never added")
	}
	MapDelete(m, a)
	sum := 0
	it := MapIter(m)
	for _, v, ok := it.Next(); ok; _, v, ok = it.Next() {
		sum += v
	}
	if sum != 10 {
		t.Fatalf("iteration sum %d, want 10", sum)
	}
}

func TestMapInterfaceKeys(t *testing.T) {
	m := NewMap[named, string](4)
	a, b := &node{1}, &node{2}
	m.Set(a, "a")
	m.Set(b, "b")
	if MapAt(m, named(a)) != "a" || MapAt(m, named(b)) != "b" {
		t.Fatalf("interface keys lost their values")
	}
	k := NewMap[any, int](0)
	k.Set("x", 1)
	k.Set(int64(7), 2)
	if MapAt(k, any("x")) != 1 || MapAt(k, any(int64(7))) != 2 || MapLen(k) != 2 {
		t.Fatalf("any keys: %d %d", MapAt(k, any("x")), MapAt(k, any(int64(7))))
	}
}

func TestNilMap(t *testing.T) {
	var m *hash.Map[string, int]
	if MapAt(m, "x") != 0 || MapLen(m) != 0 {
		t.Fatalf("nil map reads")
	}
	it := MapIter(m)
	if _, _, ok := it.Next(); ok {
		t.Fatalf("nil map iterates")
	}
}

func TestMapOf(t *testing.T) {
	m := MapOf(Pair[string, int]{"one", 1}, Pair[string, int]{"two", 2})
	if MapAt(m, "two") != 2 || MapLen(m) != 2 {
		t.Fatalf("MapOf")
	}
}

func TestAppendConcat(t *testing.T) {
	var s []int
	for i := 0; i < 100; i++ {
		s = Append(s, i)
	}
	if len(s) != 100 || s[99] != 99 {
		t.Fatalf("Append: len %d", len(s))
	}
	b := AppendString(nil, "ab")
	b = AppendString(b, "cd")
	if String(b) != "abcd" {
		t.Fatalf("AppendString: %s", String(b))
	}
	if Concat("x", "", "yz", "!") != "xyz!" || Concat() != "" {
		t.Fatalf("Concat")
	}
	if RuneString('é') != "é" || Join([]string{"a", "b", "c"}, ", ") != "a, b, c" {
		t.Fatalf("RuneString or Join")
	}
	c := Bytes("hi")
	c[0] = 'H'
	if String(c) != "Hi" {
		t.Fatalf("Bytes")
	}
}

func TestSprintfErrorf(t *testing.T) {
	if s := Sprintf("v%d %s %q", 3, "x", "y"); s != `v3 x "y"` {
		t.Fatalf("Sprintf: %s", s)
	}
	base := Errorf("base %d", 1)
	err := Errorf("wrapped %5.2f: %w", 1.5, base)
	if err.Error() != " wrapped  1.50: base 1"[1:] || !Is(err, base) || Is(base, err) {
		t.Fatalf("Errorf: %s", err.Error())
	}
	var b Buffer
	b.WriteString("abc")
	s := b.String()
	for i := 0; i < 64; i++ {
		b.WriteByte('z')
	}
	if s != "abc" || b.Len() != 67 {
		t.Fatalf("Buffer: %s %d", s, b.Len())
	}
	var le [8]byte
	PutUint64(le[:], 0x0102030405060708)
	if le[0] != 8 || le[7] != 1 {
		t.Fatalf("PutUint64")
	}
}
