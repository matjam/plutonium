package strings

import (
	"pluto/testing"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

func TestIndex(t *testing.T) {
	tests := [...]struct {
		s, sub string
		want   int
	}{
		{"", "", 0},
		{"", "a", -1},
		{"abc", "", 0},
		{"abc", "a", 0},
		{"abc", "c", 2},
		{"abc", "bc", 1},
		{"abc", "abcd", -1},
		{"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxyz", "xyz", 32},
		{"fofofofooofoboo", "oo", 7},
		{"héllo wörld", "wö", 7},
	}
	for _, tt := range tests {
		if got := Index(tt.s, tt.sub); got != tt.want {
			t.Errorf("Index(%q, %q) = %d, want %d", tt.s, tt.sub, got, tt.want)
		}
	}
}

func TestLastIndex(t *testing.T) {
	tests := [...]struct {
		s, sub string
		want   int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"abcabc", "abc", 3},
		{"abcabc", "b", 4},
		{"abc", "d", -1},
	}
	for _, tt := range tests {
		if got := LastIndex(tt.s, tt.sub); got != tt.want {
			t.Errorf("LastIndex(%q, %q) = %d, want %d", tt.s, tt.sub, got, tt.want)
		}
	}
}

func TestPrefixSuffix(t *testing.T) {
	if !HasPrefix("plutonium", "pluto") || HasPrefix("pluto", "plutonium") {
		t.Error("HasPrefix is wrong")
	}
	if !HasSuffix("plutonium", "nium") || HasSuffix("nium", "plutonium") {
		t.Error("HasSuffix is wrong")
	}
	if got := TrimPrefix("pluto/fmt", "pluto/"); got != "fmt" {
		t.Errorf("TrimPrefix = %q, want %q", got, "fmt")
	}
	if got := TrimSuffix("main_test.p", "_test.p"); got != "main" {
		t.Errorf("TrimSuffix = %q, want %q", got, "main")
	}
}

func TestCut(t *testing.T) {
	before, after, found := Cut("key=value=x", "=")
	if before != "key" || after != "value=x" || !found {
		t.Errorf("Cut = %q, %q, %t", before, after, found)
	}
	before, after, found = Cut("novalue", "=")
	if before != "novalue" || after != "" || found {
		t.Errorf("Cut without separator = %q, %q, %t", before, after, found)
	}
}

func TestTrim(t *testing.T) {
	tests := [...]struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{" \t\n x \r\n", "x"},
		{"x  y", "x  y"},
		{"  nbsp  ", "nbsp"},
	}
	for _, tt := range tests {
		if got := TrimSpace(tt.in); got != tt.want {
			t.Errorf("TrimSpace(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := Trim("xxhixx", "x"); got != "hi" {
		t.Errorf("Trim = %q, want %q", got, "hi")
	}
	if got := TrimLeft("abcba", "ab"); got != "cba" {
		t.Errorf("TrimLeft = %q, want %q", got, "cba")
	}
	if got := TrimRight("abcba", "ab"); got != "abc" {
		t.Errorf("TrimRight = %q, want %q", got, "abc")
	}
}

func TestCountAndFold(t *testing.T) {
	if got := Count("cheese", "e"); got != 3 {
		t.Errorf("Count(cheese, e) = %d, want 3", got)
	}
	if got := Count("five", ""); got != 5 {
		t.Errorf("Count(five, \"\") = %d, want 5", got)
	}
	if !EqualFold("Plutonium", "PLUTONIUM") || EqualFold("pluto", "plato") {
		t.Error("EqualFold is wrong")
	}
	if Compare("a", "b") != -1 || Compare("b", "a") != 1 || Compare("x", "x") != 0 {
		t.Error("Compare is wrong")
	}
}

func TestSplitInto(t *testing.T) {
	var out [8]string
	t.Run("sep", func(t *testing.T) {
		n := SplitInto("a,b,,c", ",", out[:])
		if n != 4 || out[0] != "a" || out[1] != "b" || out[2] != "" || out[3] != "c" {
			t.Errorf("SplitInto gave %d parts: %q %q %q %q", n, out[0], out[1], out[2], out[3])
		}
	})
	t.Run("fields", func(t *testing.T) {
		n := FieldsInto("  one two\tthree\n", out[:])
		if n != 3 || out[0] != "one" || out[1] != "two" || out[2] != "three" {
			t.Errorf("FieldsInto gave %d fields: %q %q %q", n, out[0], out[1], out[2])
		}
	})
}

// view returns the bytes of b as a string without copying them.
func view(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

func TestAppend(t *testing.T) {
	var buf [64]byte
	f := vec.NewFixed(buf[:])
	AppendUpper(&f, "MiXed")
	if got := view(f.Slice()); got != "MIXED" {
		t.Errorf("AppendUpper = %q", got)
	}
	f.Reset()
	AppendLower(&f, "MiXed")
	if got := view(f.Slice()); got != "mixed" {
		t.Errorf("AppendLower = %q", got)
	}
	f.Reset()
	AppendRepeat(&f, "ab", 3)
	if got := view(f.Slice()); got != "ababab" {
		t.Errorf("AppendRepeat = %q", got)
	}
	parts := [...]string{"a", "b", "c"}
	f.Reset()
	AppendJoin(&f, parts[:], ", ")
	if got := view(f.Slice()); got != "a, b, c" {
		t.Errorf("AppendJoin = %q", got)
	}
	f.Reset()
	AppendReplace(&f, "oink oink oink", "k", "ky", 2)
	if got := view(f.Slice()); got != "oinky oinky oink" {
		t.Errorf("AppendReplace = %q", got)
	}
	// A Fixed keeps what fits; a Vec grows.
	var small [4]byte
	g := vec.NewFixed(small[:])
	AppendRepeat(&g, "ab", 3)
	if got := view(g.Slice()); got != "abab" || !g.Truncated() {
		t.Errorf("AppendRepeat into a Fixed of 4 = %q, truncated %t", got, g.Truncated())
	}
	v := vec.New[byte]()
	defer v.Free()
	AppendRepeat(&v, "abc", 100)
	AppendJoin(&v, parts[:], "-")
	if v.Len() != 305 || view(v.Slice()[300:]) != "a-b-c" {
		t.Errorf("AppendRepeat and AppendJoin into a Vec: len %d", v.Len())
	}
}

func TestBuilder(t *testing.T) {
	var b Builder
	defer b.Free()
	b.WriteString("pluto")
	b.WriteByte('/')
	b.WriteString("strings")
	if got := b.String(); got != "pluto/strings" {
		t.Errorf("Builder.String() = %q", got)
	}
	if b.Len() != 13 {
		t.Errorf("Builder.Len() = %d, want 13", b.Len())
	}
}

func TestFromUTF8(t *testing.T) {
	tests := [...]struct {
		b     []byte
		lossy string
		valid bool
	}{
		{nil, "", true},
		{[]byte{'a', 'b'}, "ab", true},
		{[]byte{0xc3, 0xa9}, "é", true},
		{[]byte{0xf0, 0x9f, 0x98, 0x80}, "😀", true},
		{[]byte{0xff}, "�", false},
		{[]byte{0xfe}, "�", false},
		{[]byte{'a', 0xe4, 0xb8, 'b'}, "a��b", false},
		{[]byte{0xed, 0xa0, 0x80}, "���", false},
		{[]byte{0xf0, 0x9f, 0x98}, "���", false},
	}
	for _, tt := range tests {
		got := FromUTF8Lossy(tt.b)
		if got != tt.lossy {
			t.Errorf("FromUTF8Lossy(%x) = %q, want %q", tt.b, got, tt.lossy)
		}
		s, err := FromUTF8(tt.b)
		switch {
		case tt.valid && (err != nil || s != tt.lossy):
			t.Errorf("FromUTF8(%x) = %q, %v, want %q, nil", tt.b, s, err, tt.lossy)
		case !tt.valid && (err != ErrInvalidUTF8 || s != ""):
			t.Errorf("FromUTF8(%x) = %q, %v, want an empty string and ErrInvalidUTF8", tt.b, s, err)
		}
	}
	// The result is a copy: changing the bytes leaves the string alone.
	b := []byte{'c', 'o', 'p', 'y'}
	s := FromUTF8Lossy(b)
	b[0] = 'C'
	if s != "copy" {
		t.Errorf("FromUTF8Lossy shares its argument: %q", s)
	}
	// Different invalid bytes give equal strings, which is why the
	// conversion has a name that says so.
	if FromUTF8Lossy([]byte{0xfe}) != FromUTF8Lossy([]byte{0xff}) {
		t.Errorf("FromUTF8Lossy(0xfe) != FromUTF8Lossy(0xff)")
	}
}

type sliceName string

func TestSliceAndSliceFrom(t *testing.T) {
	const s = "añ€😀z" // runes of 1, 2, 3, 4 and 1 bytes, at offsets 0, 1, 3, 6, 10
	tests := [...]struct {
		i, j int
		want string
	}{
		{0, 0, ""},
		{0, 1, "a"},
		{1, 3, "ñ"},
		{3, 6, "€"},
		{6, 10, "😀"},
		{0, 11, s},
		{10, 11, "z"},
		{11, 11, ""},
	}
	for _, tt := range tests {
		if got := Slice(s, tt.i, tt.j); got != tt.want {
			t.Errorf("Slice(%q, %d, %d) = %q, want %q", s, tt.i, tt.j, got, tt.want)
		}
		if got := SliceFrom(s, tt.i); got != Slice(s, tt.i, len(s)) {
			t.Errorf("SliceFrom(%q, %d) = %q", s, tt.i, got)
		}
	}
	// The result has the type of the argument.
	var n sliceName = Slice(sliceName("héllo"), 1, 3)
	if n != "é" {
		t.Errorf("Slice of a named string = %q", string(n))
	}
	if i := Index(s, "😀"); SliceFrom(s, i+len("😀")) != "z" {
		t.Errorf("the offset after a match is not a boundary")
	}
}

func TestTruncate(t *testing.T) {
	const s = "añ€😀z"
	want := [...]string{"", "", "a", "a", "añ", "añ", "añ", "añ€", "añ€", "añ€", "añ€", "añ€😀", s, s}
	for n := -1; n < len(want)-1; n++ {
		if got := Truncate(s, n); got != want[n+1] {
			t.Errorf("Truncate(%q, %d) = %q, want %q", s, n, got, want[n+1])
		}
	}
	if got := Truncate("abc", 3); got != "abc" || Truncate("abc", 0) != "" || Truncate("", 5) != "" {
		t.Errorf("Truncate of ASCII: %q", got)
	}
}

func TestRuneStartBeforeSlice(t *testing.T) {
	const s = "añ€😀z"
	for n := -2; n <= len(s)+2; n++ {
		i := utf8.RuneStartBefore(s, n)
		if i < 0 || i > len(s) || (i < len(s) && !utf8.RuneStart(s[i])) || i > max(n, 0) || (i < n && n <= len(s) && i < n-3) {
			t.Errorf("RuneStartBefore(%q, %d) = %d", s, n, i)
		}
		if Slice(s, 0, i)+SliceFrom(s, i) != s {
			t.Errorf("the pieces at %d do not make %q", i, s)
		}
	}
}

func BenchmarkIndex(b *testing.B) {
	s := "the quick brown fox jumps over the lazy dog"
	for i := 0; i < b.N; i++ {
		if Index(s, "lazy") != 35 {
			b.Fatal("wrong index")
		}
	}
}
