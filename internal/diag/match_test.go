package diag

import (
	"math/rand"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The expressions the hand-written matchers of match.go replace.
var (
	reCannotUse    = regexp.MustCompile(`^cannot use (.+) as (.+?) value in (.+?)(?:: (.+))?$`)
	reConsumesRecv = regexp.MustCompile(`^(.+) does not implement (.+?) \(method (\w+) has a value receiver of owner type`)
	reDoesNotImpl  = regexp.MustCompile(`^(.+) does not implement (.+?) \((missing method|method) (\w+)(\)| has pointer receiver\))`)
	reOverflow     = regexp.MustCompile(` \((overflows|truncated)\)$`)
	reUndefName    = regexp.MustCompile(`^undefined: (\w+)`)
	reUndefQual    = regexp.MustCompile(`^undefined: (\w+)\.(\w+)`)
	reInfer        = regexp.MustCompile(`^in call to (.+), cannot infer (\w+)`)
	reUnusedVar    = regexp.MustCompile(`declared and not used: (\w+)|^(\w+) declared and not used`)
	reUnusedImport = regexp.MustCompile(`^"([^"]+)" imported (?:as (\w+) )?and not used`)
	reMismatched   = regexp.MustCompile(`\(mismatched types (.+) and (.+)\)$`)
	reUndefinedOp  = regexp.MustCompile(`operator (\S+) not defined on (.+)$`)
	reUndefinedRef = regexp.MustCompile("undefined reference to `([^']*)'")
	reInFunction   = regexp.MustCompile("in function `([^']*)'")
	reRelocTrunc   = regexp.MustCompile("relocation truncated to fit: (R_X86_64_[A-Z0-9]+) against (?:symbol )?`?([^' ]*)'?")
	reMultipleDef  = regexp.MustCompile("multiple definition of `([^']*)'")
	matchFragments = []string{
		"cannot use ", " as ", " value in ", ": ", "x", "int", "(", ")", " ", "  ", "a_b", "\"", "fmt", ".", ",",
		" does not implement ", " (method ", "missing method", "method", " has a value receiver of owner type",
		" has pointer receiver)", "undefined: ", "in call to ", ", cannot infer ", "declared and not used",
		"declared and not used: ", " declared and not used", " imported ", "as ", "and not used", "\"fmt\"",
		"(mismatched types ", " and ", "operator ", "+", "<=", " not defined on ", " (overflows)", " (truncated)",
		"é", "T", "7", "[]int", "*", "\t",
	}
	toolFragments = []string{
		"undefined reference to `", "in function `", "relocation truncated to fit: ", "R_X86_64_", "32S", "PC32", " against ",
		"symbol ", "`", "'", " ", "\n", "multiple definition of `", "foo", "main.main", ".bss", "x1", "(", ")",
	}
)

func randText(r *rand.Rand, frags []string) string {
	var sb strings.Builder
	for n := r.Intn(14); n > 0; n-- {
		sb.WriteString(frags[r.Intn(len(frags))])
	}
	return sb.String()
}

// TestMatchers checks the hand-written matchers against the regular
// expressions they replace, on messages the checker really reports and on
// generated text made of the pieces the expressions are built from.
func TestMatchers(t *testing.T) {
	real := []string{
		"cannot use name (variable of type string) as int value in assignment",
		"cannot use x (variable of type T) as I value in argument to f: T does not implement I (missing method M)",
		"cannot use s (variable of type S) as I value in variable declaration: S does not implement I (method M has pointer receiver)",
		"cannot use o (variable of type Owner) as I value in argument to f: Owner does not implement I (method M has a value receiver of owner type)",
		"cannot use 300 (untyped int constant) as byte value in assignment (overflows)",
		"cannot use 1.5 (untyped float constant) as int value in variable declaration (truncated)",
		"undefined: foo", "undefined: fmt.Printl", "undefined: ", "undefined: a.", "undefined: a.b.c",
		"in call to f, cannot infer T", "in call to a, b, cannot infer T, cannot infer U",
		"declared and not used: x", "x declared and not used", "declared and not used: ", "y declared and not used: x",
		`"os" imported and not used`, `"os" imported as o and not used`, `"" imported and not used`,
		"invalid operation: a + b (mismatched types int and string)",
		"invalid operation: operator + not defined on s (variable of type S)",
		"invalid operation: operator ! not defined on x (variable of type int) and more",
	}
	hits := map[string]int{}
	check := func(s string) {
		t.Helper()
		same := func(name string, want []string, got []string) {
			t.Helper()
			if want != nil {
				hits[name]++
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("%s(%q):\n got %q\nwant %q", name, s, got, want)
			}
		}
		same("matchCannotUse", reCannotUse.FindStringSubmatch(s), matchCannotUse(s))
		same("matchConsumesRecv", reConsumesRecv.FindStringSubmatch(s), matchConsumesRecv(s))
		same("matchDoesNotImpl", reDoesNotImpl.FindStringSubmatch(s), matchDoesNotImpl(s))
		same("matchUndefinedName", reUndefName.FindStringSubmatch(s), matchUndefinedName(s))
		same("matchUndefinedQualified", reUndefQual.FindStringSubmatch(s), matchUndefinedQualified(s))
		same("matchInfer", reInfer.FindStringSubmatch(s), matchInfer(s))
		same("matchUnusedVar", reUnusedVar.FindStringSubmatch(s), matchUnusedVar(s))
		same("matchUnusedImport", reUnusedImport.FindStringSubmatch(s), matchUnusedImport(s))
		same("matchUndefinedOp", reUndefinedOp.FindStringSubmatch(s), matchUndefinedOp(s))
		if got, want := trimOverflowSuffix(s), reOverflow.ReplaceAllString(s, ""); got != want {
			t.Errorf("trimOverflowSuffix(%q) = %q, want %q", s, got, want)
		}
		if got, want := isMismatched(s), reMismatched.MatchString(s); got != want {
			t.Errorf("isMismatched(%q) = %t, want %t", s, got, want)
		}
	}
	checkTool := func(s string) {
		t.Helper()
		for _, c := range []struct {
			name   string
			re     *regexp.Regexp
			before string
		}{{"undefined reference", reUndefinedRef, "undefined reference to `"}, {"in function", reInFunction, "in function `"},
			{"multiple definition", reMultipleDef, "multiple definition of `"}} {
			m := c.re.FindStringSubmatch(s)
			got, ok := matchQuoted(s, c.before)
			if (m != nil) != ok || ok && m[1] != got {
				t.Errorf("%s(%q) = %q, %t; want %q", c.name, s, got, ok, m)
			}
		}
		m := reRelocTrunc.FindStringSubmatch(s)
		reloc, sym, ok := matchRelocTrunc(s)
		if (m != nil) != ok || ok && (m[1] != reloc || m[2] != sym) {
			t.Errorf("matchRelocTrunc(%q) = %q, %q, %t; want %q", s, reloc, sym, ok, m)
		}
	}
	for _, s := range real {
		check(s)
	}
	for _, s := range []string{
		"ld: main.o: in function `main.main':\nmain.p:3:(.text+0x1): undefined reference to `main.sum'\n",
		"x.o: undefined reference to `strlen'\n", "undefined reference to `unterminated",
		"main.o:(.text+0x5): relocation truncated to fit: R_X86_64_32S against symbol `buf' defined in .bss section in main.o\n",
		"relocation truncated to fit: R_X86_64_PC32 against `.text'", "relocation truncated to fit: R_X86_64_ against x",
		"b.o: multiple definition of `memcpy'; a.o: first defined here\n",
	} {
		checkTool(s)
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 400000; i++ {
		check(randText(r, matchFragments))
		// Also a real message with one edit.
		s := real[r.Intn(len(real))]
		if n := len(s); n > 0 {
			k := r.Intn(n)
			switch r.Intn(3) {
			case 0:
				s = s[:k] + s[k+1:]
			case 1:
				s = s[:k] + matchFragments[r.Intn(len(matchFragments))] + s[k:]
			default:
				s = s[:k]
			}
			check(s)
		}
		checkTool(randText(r, toolFragments))
	}
	// The generated text must reach the matches, or the comparison proves nothing.
	for name, n := range hits {
		t.Logf("%s: %d matches", name, n)
		if n < 500 {
			t.Errorf("%s matched only %d generated texts", name, n)
		}
	}
}
