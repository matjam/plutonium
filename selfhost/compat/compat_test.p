package compat

import "pluto/testing"

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
	if r := Runes("aé世"); len(r) != 3 || r[0] != 'a' || r[1] != 'é' || r[2] != '世' || len(Runes("")) != 0 {
		t.Fatalf("Runes")
	}
	if String([]byte{'a', 0xff}) != "a�" {
		t.Fatalf("String is the lossy conversion")
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
	app := AppendUint64(AppendUint32(AppendUint16(nil, 0x0102), 0x03040506), 0x0708090a0b0c0d0e)
	want := []byte{2, 1, 6, 5, 4, 3, 0x0e, 0x0d, 0x0c, 0x0b, 0x0a, 9, 8, 7}
	if len(app) != len(want) {
		t.Fatalf("AppendUint: %d bytes", len(app))
	}
	for i := range want {
		if app[i] != want[i] {
			t.Fatalf("AppendUint: byte %d is %d, want %d", i, app[i], want[i])
		}
	}
}
