package compat

import (
	"pluto/os"
	"pluto/strings"
	"pluto/testing"
)

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

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSplitFields(t *testing.T) {
	if !eqStrings(Split("a\nb\n\nc", "\n"), []string{"a", "b", "", "c"}) || !eqStrings(Split("", ","), []string{""}) ||
		!eqStrings(Split("abc", ""), []string{"a", "b", "c"}) || len(Split("", "")) != 0 || !eqStrings(Split("xyz", "q"), []string{"xyz"}) {
		t.Fatalf("Split")
	}
	if !eqStrings(SplitN("a,b,c,d", ",", 2), []string{"a", "b,c,d"}) || SplitN("a,b", ",", 0) != nil ||
		!eqStrings(SplitN("a,b", ",", 99), []string{"a", "b"}) || !eqStrings(SplitN("é世x", "", 2), []string{"é", "世x"}) {
		t.Fatalf("SplitN")
	}
	if !eqStrings(Fields("  a  bb\t c\n"), []string{"a", "bb", "c"}) || len(Fields("   ")) != 0 || len(Fields("")) != 0 {
		t.Fatalf("Fields")
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

func TestInsertCaller(t *testing.T) {
	s := []int{1, 2, 5}
	s = Insert(s, 2, 3, 4)
	if len(s) != 5 || s[0] != 1 || s[1] != 2 || s[2] != 3 || s[3] != 4 || s[4] != 5 {
		t.Fatalf("Insert in the middle: %v", s)
	}
	s = Insert(s, 5, 6)
	s = Insert(s, 0)
	s = Insert(s, 0, 0)
	if len(s) != 7 || s[0] != 0 || s[6] != 6 {
		t.Fatalf("Insert at the ends: %v", s)
	}
	if _, _, _, ok := Caller(1); ok {
		t.Fatalf("Caller reported a caller")
	}
}

func TestTempFiles(t *testing.T) {
	dir := TempDir()
	a, err := CreateTemp(dir, "compat-*.tmp")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	b, err := CreateTemp(dir, "compat-*.tmp")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if a.Name() == b.Name() || !strings.HasSuffix(a.Name(), ".tmp") {
		t.Fatalf("CreateTemp names: %s %s", a.Name(), b.Name())
	}
	if n, err := a.Write(Bytes("hello")); n != 5 || err != nil {
		t.Fatalf("Write: %d %v", n, err)
	}
	_ = a.Close()
	_ = b.Close()
	data, err := ReadFile(a.Name())
	if err != nil || String(data) != "hello" {
		t.Fatalf("ReadFile: %q %v", String(data), err)
	}
	_ = os.Remove(a.Name())
	_ = os.Remove(b.Name())
	if exe, err := Executable(); err != nil || exe == "" {
		t.Fatalf("Executable: %q %v", exe, err)
	}
}

func TestEncodings(t *testing.T) {
	// FIPS 180-4 examples: one block, two blocks (56 bytes), and empty.
	cases := []struct{ in, want string }{
		{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq", "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"},
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	for _, c := range cases {
		sum := Sum256(Bytes(c.in))
		if got := HexEncode(sum[:]); got != c.want {
			t.Fatalf("Sum256(%q) = %s, want %s", c.in, got, c.want)
		}
	}
	vals := []int64{0, 1, -1, 63, -64, 64, 300, -300, 1 << 40, -(1 << 62)}
	var b []byte
	for _, v := range vals {
		b = AppendVarint(b, v)
	}
	b = AppendUvarint(b, 1<<63)
	for _, v := range vals {
		got, n := Varint(b)
		if n <= 0 || got != v {
			t.Fatalf("Varint: %d (%d bytes), want %d", got, n, v)
		}
		b = b[n:]
	}
	if u, n := Uvarint(b); u != 1<<63 || n != len(b) {
		t.Fatalf("Uvarint: %d (%d bytes)", u, n)
	}
	var buf [MaxVarintLen64]byte
	if n := PutVarint(buf[:], -300); n != 2 {
		t.Fatalf("PutVarint: %d bytes", n)
	}
	if _, n := Uvarint([]byte{0x80, 0x80}); n != 0 {
		t.Fatalf("Uvarint of a short buffer: %d", n)
	}
}

// TestTime checks the monotonic clock and that durations print as
// time.Duration prints them.
func TestTime(t *testing.T) {
	t0 := Now()
	if d := Since(t0); d < 0 {
		t.Fatalf("Since went back: %d", int64(d))
	}
	cases := []struct {
		d    Duration
		want string
	}{
		{0, "0s"}, {800, "800ns"}, {1000, "1µs"}, {12345, "12.345µs"}, {250000000, "250ms"}, {1500000000, "1.5s"},
		{61000000000, "1m1s"}, {90500000000, "1m30.5s"}, {7200000000000, "2h0m0s"}, {-2000, "-2µs"}, {1000001, "1.000001ms"},
	}
	for _, c := range cases {
		if got := c.d.String(); got != c.want {
			t.Fatalf("Duration(%d).String() = %s, want %s", int64(c.d), got, c.want)
		}
	}
}

// TestOnce checks that Once runs its function the first time only.
func TestOnce(t *testing.T) {
	var o Once
	n := 0
	for i := 0; i < 3; i++ {
		o.Do(func() { n++ })
	}
	if n != 1 {
		t.Fatalf("Once ran %d times", n)
	}
}
