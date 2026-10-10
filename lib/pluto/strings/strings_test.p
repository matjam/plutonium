package strings

import (
	"pluto/testing"
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
	if got := view(AppendUpper(buf[:0], "MiXed")); got != "MIXED" {
		t.Errorf("AppendUpper = %q", got)
	}
	if got := view(AppendLower(buf[:0], "MiXed")); got != "mixed" {
		t.Errorf("AppendLower = %q", got)
	}
	if got := view(AppendRepeat(buf[:0], "ab", 3)); got != "ababab" {
		t.Errorf("AppendRepeat = %q", got)
	}
	parts := [...]string{"a", "b", "c"}
	if got := view(AppendJoin(buf[:0], parts[:], ", ")); got != "a, b, c" {
		t.Errorf("AppendJoin = %q", got)
	}
	if got := view(AppendReplace(buf[:0], "oink oink oink", "k", "ky", 2)); got != "oinky oinky oink" {
		t.Errorf("AppendReplace = %q", got)
	}
}

func TestBuilder(t *testing.T) {
	var buf [32]byte
	b := NewBuilder(buf[:0])
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

func BenchmarkIndex(b *testing.B) {
	s := "the quick brown fox jumps over the lazy dog"
	for i := 0; i < b.N; i++ {
		if Index(s, "lazy") != 35 {
			b.Fatal("wrong index")
		}
	}
}
