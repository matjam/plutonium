// Ported from internal/build/stringslice_test.go at the go-cutover tag.

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStringsSliceInline checks that a call of strings.Slice or
// strings.SliceFrom is expanded inline (language spec sections 7.3 and
// 11.12, appendix F.6): no call of the library function remains in the
// code of a function that uses one, at any optimisation level, whatever the
// string type, and a cut at bounds the program has proven keeps no
// rune-boundary check. The program also runs.
func TestStringsSliceInline(t *testing.T) {
	t.Parallel()
	const src = `package main

import "pluto/strings"

type Name string

//pu:noinline
func middle(s string) string { return strings.Slice(s, 1, len(s)-1) }

//pu:noinline
func rest(s string, i int) string { return strings.SliceFrom(s, i) }

//pu:noinline
func named(n Name) Name { return strings.SliceFrom(n, 1) }

//pu:noinline
func generic[S ~string](s S) S { return strings.Slice(s, 0, 2) }

//pu:noinline
func constant() string { return strings.Slice("abcdef", 1, 3) }

//pu:noinline
func afterMatch(s, sep string) string {
	i := strings.Index(s, sep)
	if i < 0 {
		return ""
	}
	return strings.SliceFrom(s, i+len(sep))
}

func main() {
	println(middle("xéy"), rest("añb", 1), named("añb"), generic("ñab"), constant(), afterMatch("a,ñ", ","))
}
`
	for _, level := range []int{0, 2} {
		dir := writeTree(t, single(src))
		asm := filepath.Join(t.TempDir(), "listing.s")
		r := tryBuildDir(t, dir, optFlag(level), "-S", asm)
		if r.status != 0 {
			t.Fatalf("build: exit status %d\n%s", r.status, r.out)
		}
		data, err := os.ReadFile(asm)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, name := range []string{"middle", "rest", "named", "generic", "constant", "afterMatch"} {
			i := strings.Index(text, "\nmain."+name)
			if i < 0 {
				t.Fatalf("-O%d: main.%s not in listing", level, name)
			}
			body := text[i+1:]
			if j := strings.Index(body, "\n\n"); j >= 0 {
				body = body[:j]
			}
			if strings.Contains(body, "strings.Slice") {
				t.Errorf("-O%d: main.%s calls a strings.Slice function:\n%s", level, name, body)
			}
		}
		out, status := runExeEnv(t, r.exe)
		if status != 0 {
			t.Fatalf("-O%d: exit status %d\n%s", level, status, out)
		}
		if want := "é ñb ñb ñ bc ñ\n"; out != want {
			t.Errorf("-O%d: output %q, want %q", level, out, want)
		}
	}
}
