package build_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
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
		r, size := utf8.DecodeRuneInString(s[i:])
		n += int(r)
		i += size
	}
	return n
}

//pu:noinline
func decodeFirst(s string) string {
	_, size := utf8.DecodeRuneInString(s)
	return s[size:]
}

//pu:noinline
func ranged(s string) int {
	n := 0
	for i := range s {
		n += len(s[i:])
	}
	return n
}

//pu:noinline
func backwards(s string) int {
	n := 0
	for i := len(s); i > 0; {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
		n++
	}
	return n
}

//pu:noinline
func before(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i]
	}
	return s
}

//pu:noinline
func after(s, sep string) string {
	i := strings.Index(s, sep)
	if i == -1 {
		return ""
	}
	return s[i+len(sep):]
}

//pu:noinline
func afterComma(s string) string {
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[i+1:]
	}
	return s
}

//pu:noinline
func twice(s string, i, j int) int {
	a := s[i:j]
	b := s[i:]
	return len(a) + len(b)
}

//pu:noinline
func unknown(s string, i int) string { return s[i:] }

//pu:noinline
func unguarded(s, sep string) string {
	i := strings.Index(s, sep)
	return s[i+len(sep):]
}

//pu:noinline
func afterByte(s string, c byte) string {
	if i := strings.IndexByte(s, c); i >= 0 {
		return s[i+1:]
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
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		var listing bytes.Buffer
		cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib(), OptLevel: level, Listing: &listing}
		if err := build.Build(cfg); err != nil {
			t.Fatalf("build: %v", err)
		}
		text := listing.String()
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
		out, status := runConc(t, cfg.Output)
		if status != 0 {
			t.Fatalf("-O%d: exit status %d\n%s", level, status, out)
		}
		const want = "9043 ñb€c,d 38 7\nañb c,d d 11\nb€c,d d €c,d\n"
		if out != want {
			t.Errorf("-O%d: output %q, want %q", level, out, want)
		}
	}
}
