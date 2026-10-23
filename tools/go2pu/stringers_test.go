package main

import (
	"strings"
	"testing"
)

// TestStringerRewrites checks which formatting calls go2pu routes to
// compat's formatter and which operands it replaces by a call of their
// String or Error method (stringers.go): the stringers demo (whose Go and
// Plutonium output TestDifferential compares) and testdata/stringerunit,
// which holds cases Plutonium cannot build.
func TestStringerRewrites(t *testing.T) {
	text := func(dir string) string {
		res, err := TranslateDir(testConfig(), dir)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, f := range res.Files {
			b.Write(f.Src)
		}
		return b.String()
	}
	demo := text("testdata/stringers")
	for _, want := range []string{
		// Strings and integers, with conversions to the Arg types.
		`compat.Sprintfb("[%v|%s|%10s|%-10s|%.3s|%8.2s|%-8.4v|%.0s]", compat.ArgS(str),`,
		`compat.ArgS(string(lb))`,
		`compat.ArgU(uint64(uint8(255)))`, `compat.ArgI(int64(int16(-1)))`, `compat.ArgB(3 > 2)`,
		// A pointer whose method dereferences it prints <nil> when nil;
		// one that handles nil, or does not use the receiver, is called.
		`compat.Sprintfb("%v|%s|%v|%s", compat.ArgS(compat.StringOf(np)), compat.ArgS(compat.StringOf(nd)), compat.ArgS((sp).String()),`,
		// Error comes first; an integer receiver's method is called.
		`compat.Sprintfb("%v|%v|%5v|%-7s|%.3v", compat.ArgS((k).String()), compat.ArgS((cd).Error()), compat.ArgS((c).String()),`,
		// %d and %c print the integer, %x the method's result.
		`compat.Sprintfb("%d|%x|%v|%c", compat.ArgI(int64(c)), compat.ArgS((c).String()), compat.ArgS((on).String()), compat.ArgI(int64(count(65))))`,
		// Left to fmt: a struct's value String, an interface, %#v, %T,
		// %+v, * widths.
		`compat.Sprintf("%v|%s|%10v|%-8s", n, n, n, n)`,
		`compat.Sprintf("%v|%v", s, e)`,
		`compat.Sprintf("%v|%#v", gostr(1), gostr(1))`,
		`compat.Sprintf("%T|%d", c, c)`,
		`compat.Sprintf("%+v|%+v", 5, c)`,
		`compat.Sprintf("%*v|%-*s", 6, c, 4, nd)`,
		// %% takes no operand.
		`compat.Sprintfb("%v %%v %s %d", compat.ArgS((c).String()), compat.ArgS(compat.StringOf(nd)), compat.ArgI(int64(5)))`,
		// Sprint's spaces go by the operand's type; Sprintln's and
		// Println's are always there.
		`compat.Sprintfb("%v %v%v%v %v %v%v%v%v", compat.ArgS((c).String()), compat.ArgS((c).String()), compat.ArgS("s"),`,
		`compat.Sprintfb("%v%v%v %v%v", compat.ArgS("a"), compat.ArgS("b"), compat.ArgI(int64(1)), compat.ArgI(int64(2)), compat.ArgS("c"))`,
		`compat.Sprint(n, n, "s", nd, 3)`,
		`compat.Sprintfb("%v %v %v %v %v %v %v\n", compat.ArgS(compat.StringOf(np)),`,
		`compat.Printfb("%v\n", compat.ArgS(compat.Sprintfb(`,
		// Errorf without %w is an error of the text; with %w it is fmt's.
		`err := compat.NewError(compat.Sprintfb("failed %v: %d", compat.ArgS(compat.StringOf(nd)), compat.ArgI(int64(7))))`,
		`fmt.Errorf("wrapped %v: %w", c, pf)`,
		`compat.Fprintfb(os.Stdout, "%s and %v\n", compat.ArgS(compat.StringOf(nd)), compat.ArgS((sp).String()))`,
		`nw, werr2 := compat.Fprintfb(os.Stdout, "%d bytes\n", compat.ArgI(int64(12)))`,
		`fmt.Fprintln(os.Stdout, n, nd)`,
		// An operand the closure conversion substitutes is wrapped too.
		`compat.Sprintfb("%v|%s|%d", compat.ArgS(compat.StringOf(nd)), compat.ArgS((c).String()), compat.ArgI(int64((*n))))`,
		// A printf-like helper's calls are left as they are.
		`l.logf("%v and %s and %d", c, nd, 2)`,
	} {
		if !strings.Contains(demo, want) {
			t.Errorf("the stringers demo's translation does not contain\n%s", want)
		}
	}
	unit := text("testdata/stringerunit")
	for _, want := range []string{
		`compat.Sprintf("%v", formatted{})`,
		`compat.Sprintf("%[1]v %[1]v", 1)`,
		`compat.Sprintf("%v", b)`,
		`compat.Sprintf("%v", s)`,
		`compat.Sprintf("%v %s", name{}, 1)`,
		`compat.Sprintf("%v %f", np, f)`,
		`compat.Sprintf("%+v %#x %05s %.2d %t", 1, 2, "s", 3, 4)`,
		`compat.Sprintf("%d %d", 1)`,
		`compat.Sprintf("%d", 1, 2)`,
		`compat.Sprintf("plain")`,
		`compat.Sprint(name{}, 1)`,
		`fmt.Errorf("%v: %w", np, nil)`,
		`rewrites("%v", pu.Box(name{}))`,
		`compat.Sprintfb("%v|%s|%d|%x|%c", compat.ArgS((kind(1)).String()), compat.ArgS(compat.StringOf(np)), compat.ArgI(int64(kind(2))), compat.ArgS((kind(3)).String()), compat.ArgI(int64(kind(65))))`,
		`compat.Sprintfb("%s %v %d %t", compat.ArgS(string(label("l"))), compat.ArgU(uint64(uint8(3))), compat.ArgI(int64('r')), compat.ArgB(true))`,
		`compat.Sprintfb("%v%v%v %v%v%v", compat.ArgS((kind(1)).String()), compat.ArgS("a"), compat.ArgI(int64(2)), compat.ArgI(int64(3)), compat.ArgS(string(label("b"))), compat.ArgS(compat.StringOf(np)))`,
		`compat.Sprintfb("%v %v %v\n", compat.ArgS((kind(1)).String()), compat.ArgS("a"), compat.ArgI(int64(2)))`,
		`compat.NewError(compat.Sprintfb("e %d", compat.ArgI(int64(4)))).Error()`,
		`compat.Fprintfb(w, "%s\n", compat.ArgS(compat.StringOf(np)))`,
		`compat.Fprintfb(w, "%v %v", compat.ArgI(int64(1)), compat.ArgI(int64(2)))`,
		`compat.Fprintfb(w, "%v %v\n", compat.ArgS("x"), compat.ArgI(int64(1)))`,
		`compat.Printfb("%d\n", compat.ArgI(int64(5)))`,
		`compat.Printfb("%v%v", compat.ArgS("a"), compat.ArgS("b"))`,
		`compat.Printfb("%v\n", compat.ArgS(compat.StringOf(np)))`,
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("stringerunit's translation does not contain\n%s\nin\n%s", want, unit)
		}
	}
}

// TestParseVerbs checks the format parser that decides which calls are
// routed.
func TestParseVerbs(t *testing.T) {
	for _, c := range []struct {
		format string
		verbs  string // the verbs, or "!" for a format left to fmt
	}{
		{"plain", ""},
		{"%v %s %%d %-5.2q %+05d", "vsqd"},
		{"%[1]v", "!"}, {"%*d", "!"}, {"%.*d", "!"}, {"%#v", "!"}, {"%", "!"}, {"%5", "!"},
		{"%é", "!"},
	} {
		specs, ok := parseVerbs(c.format)
		got := "!"
		if ok {
			got = ""
			for _, s := range specs {
				got += string(rune(s.verb))
			}
		}
		if got != c.verbs {
			t.Errorf("parseVerbs(%q) = %q, want %q", c.format, got, c.verbs)
		}
	}
	specs, _ := parseVerbs("%-05.3x")
	if s := specs[0]; !s.minus || s.zero || !s.wid || !s.prec {
		t.Errorf("parseVerbs(%%-05.3x) = %+v: want minus, a width and a precision, and no zero (- takes it)", s)
	}
}
