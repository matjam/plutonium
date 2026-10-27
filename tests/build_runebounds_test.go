// Ported from internal/build/runebounds_test.go at the go-cutover tag.

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRuneBounds checks which string slice expressions keep the check that
// their bounds are rune boundaries (opt.RuneBounds): the ones whose bounds
// the optimiser cannot prove are boundaries. The program also runs, and
// prints what the functions return for a string with multi-byte runes.
func TestRuneBounds(t *testing.T) {
	t.Parallel()
	const src = `package main

import (
	"pluto/strings"
	"pluto/unicode/utf8"
)

//pu:noinline
func decode(s string) int {
	n := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(strings.SliceFrom(s, i))
		n += int(r)
		i += size
	}
	return n
}

//pu:noinline
func decodeFirst(s string) string {
	_, size := utf8.DecodeRuneInString(s)
	return strings.SliceFrom(s, size)
}

//pu:noinline
func ranged(s string) int {
	n := 0
	for i := range s {
		n += len(strings.SliceFrom(s, i))
	}
	return n
}

//pu:noinline
func backwards(s string) int {
	n := 0
	for i := len(s); i > 0; {
		_, size := utf8.DecodeLastRuneInString(strings.Slice(s, 0, i))
		i -= size
		n++
	}
	return n
}

//pu:noinline
func before(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return strings.Slice(s, 0, i)
	}
	return s
}

//pu:noinline
func after(s, sep string) string {
	i := strings.Index(s, sep)
	if i == -1 {
		return ""
	}
	return strings.SliceFrom(s, i+len(sep))
}

//pu:noinline
func afterComma(s string) string {
	if i := strings.IndexByte(s, ','); i >= 0 {
		return strings.SliceFrom(s, i+1)
	}
	return s
}

//pu:noinline
func twice(s string, i, j int) int {
	a := strings.Slice(s, i, j)
	b := strings.SliceFrom(s, i)
	return len(a) + len(b)
}

//pu:noinline
func unknown(s string, i int) string { return strings.SliceFrom(s, i) }

//pu:noinline
func unguarded(s, sep string) string {
	i := strings.Index(s, sep)
	return strings.SliceFrom(s, i+len(sep))
}

//pu:noinline
func afterByte(s string, c byte) string {
	if i := strings.IndexByte(s, c); i >= 0 {
		return strings.SliceFrom(s, i+1)
	}
	return s
}

func main() {
	s := "añb€c,d"
	println(decode(s), decodeFirst(s), ranged(s), backwards(s))
	println(before(s, "€"), after(s, "€"), afterComma(s), twice(s, 1, 3))
	println(unknown(s, 3), unguarded(s, ","), afterByte(s, 'b'))
}
`
	proven := []string{"decode", "decodeFirst", "ranged", "backwards", "before", "after", "afterComma"}
	checked := []string{"unknown", "unguarded", "afterByte"}
	for _, level := range []int{1, 2} {
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
		calls := func(name string) int {
			i := strings.Index(text, "\nmain."+name+":\n")
			if i < 0 {
				t.Fatalf("-O%d: main.%s not in listing", level, name)
			}
			body := text[i+1:]
			if j := strings.Index(body, "\n\n"); j >= 0 {
				body = body[:j]
			}
			return strings.Count(body, "checkRuneBounds")
		}
		for _, name := range proven {
			if n := calls(name); n != 0 {
				t.Errorf("-O%d: %s checks rune boundaries (%d calls); its bounds are proven", level, name, n)
			}
		}
		// twice checks s[i:j] and then knows both bounds.
		if n := calls("twice"); n != 1 {
			t.Errorf("-O%d: twice has %d rune-boundary checks, want 1", level, n)
		}
		for _, name := range checked {
			if n := calls(name); n == 0 {
				t.Errorf("-O%d: %s does not check rune boundaries", level, name)
			}
		}
		out, status := runExeEnv(t, r.exe)
		if status != 0 {
			t.Fatalf("-O%d: exit status %d\n%s", level, status, out)
		}
		const want = "9043 ñb€c,d 38 7\nañb c,d d 11\nb€c,d d €c,d\n"
		if out != want {
			t.Errorf("-O%d: output %q, want %q", level, out, want)
		}
	}
}
