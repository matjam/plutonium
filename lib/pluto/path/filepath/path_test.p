package filepath

import (
	"pluto/os"
	"pluto/testing"
	"unsafe"
)

// The expected values come from Go's path/filepath on Unix.

func str(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

func TestClean(t *testing.T) {
	tests := [...]struct{ in, want string }{
		{"abc", "abc"}, {"abc/def", "abc/def"}, {"a/b/c", "a/b/c"}, {".", "."}, {"..", ".."},
		{"../..", "../.."}, {"../../abc", "../../abc"}, {"/abc", "/abc"}, {"/", "/"}, {"", "."},
		{"abc/", "abc"}, {"abc/def/", "abc/def"}, {"a/b/c/", "a/b/c"}, {"./", "."}, {"../", ".."},
		{"../../", "../.."}, {"/abc/", "/abc"}, {"abc//def//ghi", "abc/def/ghi"}, {"//abc", "/abc"},
		{"///abc", "/abc"}, {"//abc//", "/abc"}, {"abc//", "abc"}, {"abc/./def", "abc/def"},
		{"/./abc/def", "/abc/def"}, {"abc/.", "abc"}, {"abc/def/ghi/../jkl", "abc/def/jkl"},
		{"abc/def/../ghi/../jkl", "abc/jkl"}, {"abc/def/..", "abc"}, {"abc/def/../..", "."},
		{"/abc/def/../..", "/"}, {"abc/def/../../..", ".."}, {"/abc/def/../../..", "/"},
		{"abc/def/../../../ghi/jkl/../../../mno", "../../mno"}, {"/../abc", "/abc"},
		{"abc/./../def", "def"}, {"abc//./../def", "def"}, {"abc/../../././../def", "../../def"},
	}
	var buf [128]byte
	for _, tt := range tests {
		if got := str(AppendClean(buf[:0], tt.in)); got != tt.want {
			t.Errorf("AppendClean(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if got := Clean(tt.in); got != tt.want {
			t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
		}
		// Appending keeps what dst holds.
		if got := str(AppendClean(append(buf[:0], "x:"...), tt.in)); got[2:] != tt.want || got[:2] != "x:" {
			t.Errorf("AppendClean(\"x:\", %q) = %q", tt.in, got)
		}
	}
}

func TestJoin(t *testing.T) {
	tests := [...]struct {
		elem []string
		want string
	}{
		{[]string{}, ""}, {[]string{""}, ""}, {[]string{"a"}, "a"}, {[]string{"a", "b"}, "a/b"},
		{[]string{"a", ""}, "a"}, {[]string{"", "b"}, "b"}, {[]string{"/", "a"}, "/a"},
		{[]string{"/", ""}, "/"}, {[]string{"a/", "b"}, "a/b"}, {[]string{"a/", ""}, "a"},
		{[]string{"", ""}, ""}, {[]string{"a", "../b", "./c"}, "b/c"}, {[]string{"/x", "..", "..", "y"}, "/y"},
	}
	var buf [128]byte
	for _, tt := range tests {
		if got := str(AppendJoin(buf[:0], tt.elem...)); got != tt.want {
			t.Errorf("AppendJoin(%d elements) = %q, want %q", len(tt.elem), got, tt.want)
		}
		if got := Join(tt.elem...); got != tt.want {
			t.Errorf("Join(%d elements) = %q, want %q", len(tt.elem), got, tt.want)
		}
	}
	// A path longer than the stack buffer.
	long := Join(Clean(string(make([]byte, 300))), "x/../y")
	if len(long) != 302 || long[300:] != "/y" {
		t.Errorf("long Join: %d bytes, ends %q", len(long), long[len(long)-2:])
	}
	// A clean path is returned as it is.
	p := "/usr/lib"
	if Clean(p) != p || Join("", p) != p {
		t.Error("Clean or Join of a clean path changed it")
	}
}

func TestBaseDirExtSplit(t *testing.T) {
	tests := [...]struct{ in, base, dir, ext, sdir, sfile string }{
		{"", ".", ".", "", "", ""},
		{".", ".", ".", ".", "", "."},
		{"/.", ".", "/", ".", "/", "."},
		{"/", "/", "/", "", "/", ""},
		{"////", "/", "/", "", "////", ""},
		{"x/", "x", "x", "", "x/", ""},
		{"abc", "abc", ".", "", "", "abc"},
		{"abc/def", "def", "abc", "", "abc/", "def"},
		{"a/b/.x", ".x", "a/b", ".x", "a/b/", ".x"},
		{"a/b/c.", "c.", "a/b", ".", "a/b/", "c."},
		{"a/b/c.x", "c.x", "a/b", ".x", "a/b/", "c.x"},
		{"a.dir/b", "b", "a.dir", "", "a.dir/", "b"},
		{"a.dir/b.go", "b.go", "a.dir", ".go", "a.dir/", "b.go"},
		{"/usr/lib/libc.so.6", "libc.so.6", "/usr/lib", ".6", "/usr/lib/", "libc.so.6"},
	}
	for _, tt := range tests {
		if got := Base(tt.in); got != tt.base {
			t.Errorf("Base(%q) = %q, want %q", tt.in, got, tt.base)
		}
		if got := Dir(tt.in); got != tt.dir {
			t.Errorf("Dir(%q) = %q, want %q", tt.in, got, tt.dir)
		}
		var buf [64]byte
		if got := str(AppendDir(buf[:0], tt.in)); got != tt.dir {
			t.Errorf("AppendDir(%q) = %q, want %q", tt.in, got, tt.dir)
		}
		if got := Ext(tt.in); got != tt.ext {
			t.Errorf("Ext(%q) = %q, want %q", tt.in, got, tt.ext)
		}
		if d, f := Split(tt.in); d != tt.sdir || f != tt.sfile {
			t.Errorf("Split(%q) = %q, %q, want %q, %q", tt.in, d, f, tt.sdir, tt.sfile)
		}
	}
	// Go's Dir cleans, and so do Dir and AppendDir.
	var buf [64]byte
	if got := str(AppendDir(buf[:0], "a/./b/../c/d")); got != "a/c" {
		t.Errorf("AppendDir = %q, want a/c", got)
	}
	if got := Dir("a/./b/../c/d"); got != "a/c" {
		t.Errorf("Dir = %q, want a/c", got)
	}
	if !IsAbs("/a") || IsAbs("a") || IsAbs("") {
		t.Error("IsAbs")
	}
}

func TestRel(t *testing.T) {
	tests := [...]struct{ root, path, want string }{
		{"a/b", "a/b", "."}, {"a/b/.", "a/b", "."}, {"a/b", "a/b/.", "."}, {"./a/b", "a/b", "."},
		{"a/b", "./a/b", "."}, {"ab/cd", "ab/cde", "../cde"}, {"ab/cd", "ab/c", "../c"},
		{"a/b", "a/b/c/d", "c/d"}, {"a/b", "a/b/../c", "../c"}, {"a/b/../c", "a/b", "../b"},
		{"a/b/c", "a/c/d", "../../c/d"}, {"a/b", "c/d", "../../c/d"}, {"a/b/c/d", "a/b", "../.."},
		{"a/b/c/d", "a/b/", "../.."}, {"a/b/c/d/", "a/b", "../.."}, {"a/b/c/d/", "a/b/", "../.."},
		{"../../a/b", "../../a/b/c/d", "c/d"}, {"/a/b", "/a/b", "."}, {"/a/b/.", "/a/b", "."},
		{"/a/b", "/a/b/.", "."}, {"/ab/cd", "/ab/cde", "../cde"}, {"/ab/cd", "/ab/c", "../c"},
		{"/a/b", "/a/b/c/d", "c/d"}, {"/a/b", "/a/b/../c", "../c"}, {"/a/b/../c", "/a/b", "../b"},
		{"/a/b/c", "/a/c/d", "../../c/d"}, {"/a/b", "/c/d", "../../c/d"}, {"/a/b/c/d", "/a/b", "../.."},
		{"/../../a/b", "/../../a/b/c/d", "c/d"}, {".", "a/b", "a/b"}, {".", "..", ".."},
		{"..", ".", "err"}, {"..", "a", "err"}, {"../..", "..", "err"}, {"a", "/a", "err"}, {"/a", "a", "err"},
	}
	var buf [128]byte
	for _, tt := range tests {
		b, err := AppendRel(buf[:0], tt.root, tt.path)
		got := str(b)
		if tt.want == "err" {
			if err == nil {
				t.Errorf("Rel(%q, %q) = %q, want an error", tt.root, tt.path, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("AppendRel(%q, %q) = %q, %v, want %q", tt.root, tt.path, got, err, tt.want)
		}
		if s, err := Rel(tt.root, tt.path); err != nil || s != tt.want {
			t.Errorf("Rel(%q, %q) = %q, %v, want %q", tt.root, tt.path, s, err, tt.want)
		}
	}
	if _, err := Rel("a", "/a"); err == nil {
		t.Error("Rel(a, /a): no error")
	}
}

func TestAbs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var buf, want [4096]byte
	b, err := AppendAbs(buf[:0], "x/../y")
	w := AppendJoin(want[:0], cwd, "y")
	if err != nil || str(b) != str(w) {
		t.Errorf("Abs(x/../y) = %q, %v; want %q", str(b), err, str(w))
	}
	b, _ = AppendAbs(buf[:0], "/a//b/")
	if str(b) != "/a/b" {
		t.Errorf("Abs(/a//b/) = %q", str(b))
	}
	if s, err := Abs("x/../y"); err != nil || s != str(w) {
		t.Errorf("Abs(x/../y) = %q, %v; want %q", s, err, str(w))
	}
	if s, _ := Abs("/a//b/"); s != "/a/b" {
		t.Errorf("Abs(/a//b/) = %q", s)
	}
}

func TestMatch(t *testing.T) {
	tests := [...]struct {
		pattern, s string
		match      bool
		bad        bool
	}{
		{"abc", "abc", true, false}, {"*", "abc", true, false}, {"*c", "abc", true, false},
		{"a*", "a", true, false}, {"a*", "abc", true, false}, {"a*", "ab/c", false, false},
		{"a*/b", "abc/b", true, false}, {"a*/b", "a/c/b", false, false},
		{"a*b*c*d*e*/f", "axbxcxdxe/f", true, false}, {"a*b*c*d*e*/f", "axbxcxdxexxx/f", true, false},
		{"a*b*c*d*e*/f", "axbxcxdxe/xxx/f", false, false}, {"a*b*c*d*e*/f", "axbxcxdxexxx/fff", false, false},
		{"a*b?c*x", "abxbbxdbxebxczzx", true, false}, {"a*b?c*x", "abxbbxdbxebxczzy", false, false},
		{"ab[c]", "abc", true, false}, {"ab[b-d]", "abc", true, false}, {"ab[e-g]", "abc", false, false},
		{"ab[^c]", "abc", false, false}, {"ab[^b-d]", "abc", false, false}, {"ab[^e-g]", "abc", true, false},
		{"a\\*b", "a*b", true, false}, {"a\\*b", "ab", false, false}, {"a?b", "a☺b", true, false},
		{"a[^a]b", "a☺b", true, false}, {"a???b", "a☺b", false, false}, {"a[^a][^a][^a]b", "a☺b", false, false},
		{"[a-ζ]*", "α", true, false}, {"*[a-ζ]", "A", false, false}, {"a?b", "a/b", false, false},
		{"a*b", "a/b", false, false}, {"[\\]a]", "]", true, false}, {"[\\-]", "-", true, false},
		{"[x\\-]", "x", true, false}, {"[x\\-]", "-", true, false}, {"[x\\-]", "z", false, false},
		{"[\\-x]", "x", true, false}, {"[\\-x]", "-", true, false}, {"[\\-x]", "a", false, false},
		{"[]a]", "]", false, true}, {"[-]", "-", false, true}, {"[x-]", "x", false, true},
		{"[x-]", "-", false, true}, {"[x-]", "z", false, true}, {"[-x]", "x", false, true},
		{"[-x]", "-", false, true}, {"[-x]", "a", false, true}, {"\\", "a", false, true},
		{"[a-b-c]", "a", false, true}, {"[", "a", false, true}, {"[^", "a", false, true},
		{"[^bc", "a", false, true}, {"a[", "a", false, true}, {"a[", "ab", false, true},
		{"a[", "x", false, true}, {"a/b[", "x", false, true}, {"*x", "xxx", true, false},
	}
	for _, tt := range tests {
		ok, err := Match(tt.pattern, tt.s)
		if ok != tt.match || (err != nil) != tt.bad {
			t.Errorf("Match(%q, %q) = %v, %v; want %v, bad %v", tt.pattern, tt.s, ok, err, tt.match, tt.bad)
		}
	}
}

func TestWalkDir(t *testing.T) {
	root, err := os.MkdirTemp("", "walk-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	var pb [512]byte
	for _, p := range [...]string{"b/x", "a", "c/d/e", "skip/y"} {
		os.MkdirAll(str(AppendJoin(pb[:0], root, p)), 0o755)
	}
	for _, p := range [...]string{"a/1.txt", "b/x/2.txt", "c/3.txt", "skip/z.txt", "top.txt"} {
		os.WriteFile(str(AppendJoin(pb[:0], root, p)), nil, 0o644)
	}
	var seen [32][64]byte
	var lens [32]int
	n := 0
	err = WalkDir(root, func(path string, d *os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		var rb [256]byte
		rel, _ := AppendRel(rb[:0], root, path)
		if d.IsDir() && str(rel) == "skip" {
			return SkipDir
		}
		lens[n] = copy(seen[n][:], rel)
		if d.IsDir() {
			seen[n][lens[n]] = '/'
			lens[n]++
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [...]string{"./", "a/", "a/1.txt", "b/", "b/x/", "b/x/2.txt", "c/", "c/3.txt", "c/d/", "c/d/e/", "top.txt"}
	if n != len(want) {
		t.Fatalf("walked %d paths, want %d", n, len(want))
	}
	for i, w := range want {
		if got := str(seen[i][:lens[i]]); got != w {
			t.Errorf("path %d = %q, want %q", i, got, w)
		}
	}
	// SkipAll stops the walk; a missing root is reported to fn.
	n = 0
	WalkDir(root, func(path string, d *os.DirEntry, err error) error {
		n++
		if n == 3 {
			return SkipAll
		}
		return nil
	})
	if n != 3 {
		t.Errorf("SkipAll: %d calls, want 3", n)
	}
	calls := 0
	err = WalkDir(str(AppendJoin(pb[:0], root, "missing")), func(path string, d *os.DirEntry, err error) error {
		calls++
		if d != nil || err == nil {
			t.Errorf("missing root: d %v, err %v", d != nil, err)
		}
		return nil
	})
	if err != nil || calls != 1 {
		t.Errorf("missing root: %v, %d calls", err, calls)
	}
}
