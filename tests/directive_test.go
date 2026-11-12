package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// The tests in this file check the diagnostics of the //pu: directives
// (E2600-E2604), which pluto reports when it loads a package, and that the
// directives the compiler takes are the ones section 20.3 of the
// specification lists.

// directiveErrors loads src as a package with pluto doc and returns its
// directive diagnostics (E2600-E2699) as "code: message", in order. A
// syntax error fails the test: the directives are checked only in a file
// that parses.
func directiveErrors(t *testing.T, src string) []string {
	t.Helper()
	out, _ := runPluto(t, writeProgram(t, src), nil, "doc")
	var got []string
	for _, d := range diagnostics(out) {
		switch {
		case strings.HasPrefix(d.code, "E1"):
			t.Fatalf("parse: %s: %s", d.pos, d.msg)
		case strings.HasPrefix(d.code, "E26"):
			got = append(got, d.code+": "+d.msg)
		}
	}
	return got
}

func TestDirectiveCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want []string // "code: message", in order
	}{
		{"fine", "//pu:build amd64\n\npackage p\n\n//pu:noinline\nfunc f() {}\n\n//pu:extern strlen\nfunc g()\n\n//pu:export\nfunc h() {}\n\n//pu:nochecks\nfunc k() {}\n\n//pu:percpu\nvar v int\n\nvar (\n\t//pu:scheduler\n\ts int\n\t//pu:section .data\n\tw int\n)\n\n//pu:stack 1M\nfunc main() {}\n", nil},
		{"typo", "package p\n\n//pu:interupt\nfunc f() {}\n", []string{"E2600: unknown directive //pu:interupt (did you mean //pu:interrupt?)"}},
		{"go noinline", "package p\n\n//go:noinline\nfunc f() {}\n", []string{"E2604: //go:noinline is a Go directive; write //pu:noinline"}},
		{"go build", "//go:build linux\n\npackage p\n", []string{"E2604: //go:build is a Go directive; write //pu:build"}},
		{"go noescape", "package p\n\n//go:noescape\nfunc f(p *int)\n", []string{"E2604: //go:noescape is a Go directive; write //pu:noescape, on a function without a body"}},
		{"go generate", "package p\n\n//go:generate stringer -type=T\n", []string{"E2604: //go:generate is a Go directive, and Plutonium has no equivalent; delete it"}},
		{"go linkname in a body", "package p\n\nfunc f() {\n\t//go:linkname x y\n}\n", []string{"E2604: //go:linkname is a Go directive, and Plutonium has no equivalent; delete it"}},
		{"go note with a space", "package p\n\n// go:noinline is how Go spells it\nfunc f() {}\n", nil},
		{"far", "package p\n\n//pu:frobnicate\nfunc f() {}\n", []string{"E2600: unknown directive //pu:frobnicate"}},
		{"empty", "package p\n\n//pu:\nfunc f() {}\n", []string{"E2600: //pu: names no directive"}},
		{"alloc", "package p\n\n//pu:alloc\nfunc f() {}\n", []string{"E2601: //pu:alloc was removed: kernel code may allocate strings anywhere the allocator may be called"}},
		{"bytewise", "package p\n\n//pu:bytewise\nfunc f() {}\n", []string{"E2601: //pu:bytewise was removed: the compiler proves that string slice bounds fall on rune boundaries"}},
		{"gofunc", "package p\n\n//pu:gofunc\nfunc f() {}\n", []string{"E2601: //pu:gofunc was removed: the compiler finds which functions start tasks with their func arguments"}},
		{"schedhook", "package p\n\n//pu:schedhook\nfunc f()\n\n//pu:schedlate\nfunc g()\n", []string{
			"E2601: //pu:schedhook was removed: the compiler knows the runtime's scheduler hooks by name",
			"E2601: //pu:schedlate was removed: the compiler knows the runtime's scheduler hooks by name"}},
		{"nobounds", "package p\n\n//pu:nobounds\nfunc f() {}\n", []string{"E2601: //pu:nobounds was replaced by //pu:nochecks"}},
		{"nochecks arg", "package p\n\n//pu:nochecks fast\nfunc f() {}\n", []string{`E2603: //pu:nochecks takes no argument, found "fast"`}},
		{"noreturn body", "package p\n\n//pu:noreturn\nfunc f() {\n\tfor {\n\t}\n}\n", []string{"E2601: //pu:noreturn is inferred for a function with a body: delete it"}},
		{"noreturn bodyless", "package p\n\n//pu:noreturn\nfunc f()\n", nil},
		{"noescape body", "package p\n\n//pu:noescape\nfunc f(p *int) {}\n", []string{"E2601: //pu:noescape is inferred for a function with a body: delete it"}},
		{"noescape bodyless", "package p\n\n//pu:noescape\nfunc f(p *int)\n", nil},
		{"noreturn on var", "package p\n\n//pu:noreturn\nvar v int\n", []string{"E2602: //pu:noreturn is in the wrong place: it goes in the doc comment of a function, not of a package-level variable"}},
		{"percpu on func", "package p\n\n//pu:percpu\nfunc f() {}\n", []string{"E2602: //pu:percpu is in the wrong place: it goes in the doc comment of a package-level variable, not of a function"}},
		{"inline on var", "package p\n\n//pu:inline\nvar v int\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a package-level variable"}},
		{"inline on type", "package p\n\n//pu:inline\ntype T int\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a type declaration"}},
		{"inline on grouped type", "package p\n\ntype (\n\t//pu:inline\n\tT int\n)\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a type declaration"}},
		{"inline on const", "package p\n\n//pu:inline\nconst c = 1\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a const declaration"}},
		{"inline on import", "package p\n\n//pu:inline\nimport \"unsafe\"\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of an import declaration"}},
		{"in body", "package p\n\nfunc f() {\n\t//pu:noinline\n\tf()\n}\n", []string{"E2602: //pu:noinline is in the wrong place: it goes in the doc comment of a function, directly above it; it is not above any declaration here"}},
		{"floating", "package p\n\n//pu:noinline\n\nfunc f() {}\n", []string{"E2602: //pu:noinline is in the wrong place: it goes in the doc comment of a function, directly above it; it is not above any declaration here"}},
		{"build late", "package p\n\n//pu:build amd64\nfunc f() {}\n", []string{"E2602: //pu:build is in the wrong place: it goes before the package clause, where it chooses whether the file is part of the build; here it is ignored"}},
		{"noinline before package", "//pu:noinline\n\npackage p\n", []string{"E2602: //pu:noinline is in the wrong place: it goes in the doc comment of a function; it is before the package clause here"}},
		{"interrupt", "package p\n\n//pu:interrupt\nfunc f() {}\n", nil},
		{"interrupt trap", "package p\n\n//pu:interrupt trap\nfunc f() {}\n", nil},
		{"interrupt other argument", "package p\n\n//pu:interrupt fast\nfunc f() {}\n", []string{`E2603: //pu:interrupt takes no argument but trap, found "fast"`}},
		{"interrupt trap and more", "package p\n\n//pu:interrupt trap fast\nfunc f() {}\n", []string{`E2603: //pu:interrupt takes no argument but trap, found "trap fast"`}},
		{"interrupt trap on a variable", "package p\n\n//pu:interrupt trap\nvar v int\n", []string{"E2602: //pu:interrupt is in the wrong place: it goes in the doc comment of a function, not of a package-level variable"}},
		{"arg given", "package p\n\n//pu:inline hot\nfunc f() {}\n", []string{`E2603: //pu:inline takes no argument, found "hot"`}},
		{"arg missing", "package p\n\n//pu:extern\nfunc f()\n\n//pu:section\nvar v int\n", []string{
			"E2603: //pu:extern needs an argument: the name of the C symbol",
			"E2603: //pu:section needs an argument: the name of the section"}},
		{"build no expr", "//pu:build\n\npackage p\n", []string{"E2603: //pu:build needs an argument: a build constraint such as amd64 && !kernel"}},
		{"not a directive", "package p\n\n// pu:noinline\n/* //pu:x */\nfunc f() {}\n", nil},
		{"method", "package p\n\ntype T int\n\n//pu:noinline\nfunc (T) m() {}\n", nil},
		{"section on func", "package p\n\n//pu:section .text.hot\nfunc f() {}\n", nil},
		// The argument of a directive is the text after its name, without
		// the spaces and tabs around it.
		{"extern spaces", "package p\n\n//pu:extern  strlen \nfunc f()\n", nil},
		{"extern tab", "package p\n\n//pu:extern\tstrlen\nfunc f()\n", nil},
		{"interrupt spaces", "package p\n\n//pu:interrupt  trap\nfunc f() {}\n", nil},
		{"interrupt tab", "package p\n\n//pu:interrupt\ttrap\nfunc f() {}\n", nil},
		{"interrupt mixed", "package p\n\n//pu:interrupt \t trap\nfunc f() {}\n", nil},
		{"interrupt trailing", "package p\n\n//pu:interrupt\t trap \nfunc f() {}\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := directiveErrors(t, tt.src)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// retiredDirectives are the directives that were removed, which E2601
// reports (see TestDirectiveCheck).
var retiredDirectives = []string{"alloc", "bytewise", "gofunc", "schedhook", "schedlate", "nobounds"}

// specDirectives returns the directives that the tables of section 20.3 of
// the specification list, by name, and the forms written with an argument
// that the table lists as rows of their own (such as `//pu:interrupt trap`),
// and the text of the specification.
func specDirectives(t *testing.T) (names, forms []string, spec string) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "docs", "spec", "language", "20-system-considerations.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(text)
	i := strings.Index(s, "### 20.3 Directives")
	j := strings.Index(s, "### 20.4 ")
	if i < 0 || j < i {
		t.Fatal("section 20.3 not found in the specification")
	}
	row := regexp.MustCompile("(?m)^\\| `//pu:([a-z]+)([^`]*)`")
	seen := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(s[i:j], -1) {
		if m[2] == " trap" {
			forms = append(forms, m[1]+m[2])
		}
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	slices.Sort(names)
	return names, forms, s
}

// compilerDirectives returns the directives the compiler takes, from the
// list its E2600 diagnostic gives.
func compilerDirectives(t *testing.T) []string {
	t.Helper()
	out, _ := runPluto(t, writeProgram(t, "package p\n\n//pu:frobnicate\nfunc f() {}\n"), nil, "doc")
	var text []string
	for _, line := range strings.Split(out, "\n") {
		text = append(text, strings.TrimSpace(line))
	}
	m := regexp.MustCompile(`The directives are: ([a-z, ]+)\.`).FindStringSubmatch(strings.Join(text, " "))
	if m == nil {
		t.Fatalf("no list of directives in the E2600 diagnostic:\n%s", out)
	}
	return strings.Split(m[1], ", ")
}

// TestSpecDirectives is the directive audit: the directives the compiler
// accepts are exactly the ones the tables of section 20.3 list, the form of
// //pu:interrupt that the specification lists is the one the compiler takes
// (TestDirectiveCheck checks the arguments), and the catalog explains every
// directive and every removed one.
func TestSpecDirectives(t *testing.T) {
	t.Parallel()
	names, forms, spec := specDirectives(t)
	got := compilerDirectives(t)
	// build is written before the package clause and has its own table row.
	if !slices.Equal(got, names) {
		t.Errorf("the compiler's directives differ from section 20.3\ncompiler: %v\nspec:     %v", got, names)
	}
	if want := []string{"interrupt trap"}; !slices.Equal(forms, want) {
		t.Errorf("forms of directives in section 20.3: %v, want %v", forms, want)
	}
	// Every retired directive is named in the specification as retired, and
	// none is also a directive.
	for _, n := range retiredDirectives {
		if slices.Contains(names, n) {
			t.Errorf("%s is both listed and retired", n)
		}
		if !strings.Contains(spec, n) {
			t.Errorf("the specification does not mention the retired directive %s", n)
		}
	}
	// The catalog lists the valid directives and the removed ones.
	text, _ := runPluto(t, ".", nil, "explain", "E2600")
	for _, n := range got {
		if !strings.Contains(text, n) {
			t.Errorf("the E2600 catalog text does not list the directive %s", n)
		}
	}
	text, _ = runPluto(t, ".", nil, "explain", "E2601")
	for _, n := range retiredDirectives {
		if !strings.Contains(text, "//pu:"+n) {
			t.Errorf("the E2601 catalog text does not list the removed directive %s", n)
		}
	}
}

// TestResultHoldsBadName checks that a //pu:resultholds directive in the
// standard library that names something other than a parameter of its
// function is an error (E3007), rather than a claim the escape analysis
// cannot use. The directive is added to a copy of the library.
func TestResultHoldsBadName(t *testing.T) {
	t.Parallel()
	const extra = "package strings\n\n//pu:resultholds q\nfunc ZZHolds(p *int) *int { return p }\n"
	lib := stdlibWith(t, fstest.MapFS{"pluto/strings/zz_resultholds.p": &fstest.MapFile{Data: []byte(extra)}})
	dir := writeProgram(t, "package main\n\nimport \"pluto/strings\"\n\nfunc main() {\n\tx := 1\n\tprintln(*strings.ZZHolds(&x))\n}\n")
	out, status := runPluto(t, dir, []string{"PLUTOROOT=" + lib}, "build", "-o", filepath.Join(t.TempDir(), "prog"), ".")
	if status == 0 || !strings.Contains(out, "//pu:resultholds names q, which is not a parameter of ZZHolds") || !strings.Contains(out, "pluto explain E3007") {
		t.Errorf("exit status %d, output:\n%s\nwant the E3007 error", status, out)
	}
}
