package compat

import (
	"pluto/alloc"
	"pluto/hash"
	"pluto/testing"
)

type node struct{ id int }

// newNode allocates a node: a map outlives the test's frame, so its keys
// may not point into it (&node{1} would).
func newNode(id int) *node {
	n := alloc.New[node]() // never freed, as in Go
	n.id = id
	return n
}

// Interface keys stored in a map are package-level, so that their boxes
// are static rather than in the test's frame.
var (
	keyX     any = "x"
	keySeven any = int64(7)
)

type named interface{ name() string }

func (n *node) name() string { return "node" }

func TestMapPointerKeys(t *testing.T) {
	m := NewMap[*node, int](0)
	a, b := newNode(1), newNode(2)
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
	a, b := newNode(1), newNode(2)
	m.Set(a, "a")
	m.Set(b, "b")
	if MapAt(m, named(a)) != "a" || MapAt(m, named(b)) != "b" {
		t.Fatalf("interface keys lost their values")
	}
	k := NewMap[any, int](0)
	k.Set(keyX, 1)
	k.Set(keySeven, 2)
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

type edge struct{ a, b int32 }

func hashEdge(k edge) uint64 { return Mix(Mix(0, HashAny(k.a)), HashAny(k.b)) }

func TestMapFunc(t *testing.T) {
	m := NewMapFunc[edge, bool](0, hashEdge)
	for i := int32(0); i < 50; i++ {
		m.Set(edge{i, i + 1}, true)
	}
	if !MapAt(m, edge{7, 8}) || MapAt(m, edge{8, 7}) || MapLen(m) != 50 {
		t.Fatalf("struct keys")
	}
	k := MapOfFunc(func(k [4]byte) uint64 { return HashBytes(k[:]) }, Pair[[4]byte, int]{[4]byte{1, 2, 3, 4}, 9})
	if MapAt(k, [4]byte{1, 2, 3, 4}) != 9 || HashAny("x") != HashAny("x") || HashAny(1) == HashAny(2) {
		t.Fatalf("array keys or HashAny")
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
	b = AppendInt(b, -42, 10)
	b = AppendRune(b, 'é')
	b = AppendFloat(b, 1.5, 'g', -1, 64)
	b = Appendf(b, "|%d|%s", 7, "z")
	if String(b) != "abcd-42é1.5|7|z" {
		t.Fatalf("Append wrappers: %s", String(b))
	}
	m := MakeCap[string](2, 5)
	if len(m) != 2 || cap(m) != 5 || m[1] != "" || len(Make[int](0)) != 0 {
		t.Fatalf("MakeCap: %d %d", len(m), cap(m))
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

func TestSprintfNewError(t *testing.T) {
	if s := Sprintf("v%d %s %q", 3, "x", "y"); s != `v3 x "y"` {
		t.Fatalf("Sprintf: %s", s)
	}
	e1, e2 := NewError("made at run time"), NewError("made at run time")
	if e1.Error() != "made at run time" || e1 == e2 {
		t.Fatalf("NewError: %s, distinct %t", e1.Error(), e1 != e2)
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
