package regexp

import (
	"pluto/testing"
)

type matchTest struct {
	expr string
	s    string
	want bool
}

// The expected results are those of Go's regexp.MatchString.
var matchTests = []matchTest{
	{``, ``, true},
	{``, `abc`, true},
	{`abc`, `xabcy`, true},
	{`abc`, `ab`, false},
	{`^abc$`, `abc`, true},
	{`^abc$`, `abc\n`, false},
	{`^$`, ``, true},
	{`^$`, `TestAdd`, false},
	{`Add|Sub`, `TestSub`, true},
	{`Add|Sub`, `TestMul`, false},
	{`Add$|Before`, `TestAdd`, true},
	{`Add$|Before`, `TestAddMore`, false},
	{`Add$|Before`, `TestBeforeAll`, true},
	{`^Test(Add|Sub)$`, `TestSub`, true},
	{`^Test(Add|Sub)$`, `TestSubX`, false},
	{`a*b`, `b`, true},
	{`a+b`, `b`, false},
	{`a+b`, `xaab`, true},
	{`colou?r`, `color`, true},
	{`colou?r`, `colour`, true},
	{`colou?r`, `colouur`, false},
	{`^a{2,3}$`, `a`, false},
	{`^a{2,3}$`, `aa`, true},
	{`^a{2,3}$`, `aaa`, true},
	{`^a{2,3}$`, `aaaa`, false},
	{`^a{2}$`, `aa`, true},
	{`^a{2,}$`, `aaaaa`, true},
	{`^a{2,}$`, `a`, false},
	{`a{,2}`, `a{,2}`, true},
	{`x{`, `x{`, true},
	{`^.$`, `é`, true},
	{`^.$`, "\n", false},
	{`(?s)^.$`, "\n", true},
	{`(?i)hello`, `HeLLo`, true},
	{`hello`, `HeLLo`, false},
	{`(?i:h)ello`, `Hello`, true},
	{`(?i:h)ello`, `HELLO`, false},
	{`(?m)^b$`, "a\nb\nc", true},
	{`^b$`, "a\nb\nc", false},
	{`[a-c]+x`, `zbcax`, true},
	{`^[^a-c]+$`, `xyz`, true},
	{`^[^a-c]+$`, `xbz`, false},
	{`(?i)^[a-c]$`, `B`, true},
	{`^[\d]+$`, `123`, true},
	{`^\d+$`, `12a`, false},
	{`^\w+$`, `Test_1`, true},
	{`^\W$`, `-`, true},
	{`^\s$`, "\t", true},
	{`^\S$`, " ", false},
	{`^[[:alpha:]]+$`, `abc`, true},
	{`^[[:^alpha:]]+$`, `a1`, false},
	{`^[\D]$`, `x`, true},
	{`\bfoo\b`, `a foo b`, true},
	{`\bfoo\b`, `afoob`, false},
	{`\Bfoo\B`, `afoob`, true},
	{`\Aabc\z`, `abc`, true},
	{`a\.b`, `a.b`, true},
	{`a\.b`, `axb`, false},
	{`\Qa.b\E`, `a.b`, true},
	{`\Qa.b\E`, `axb`, false},
	{`\x41\x{42}`, `AB`, true},
	{`^\101$`, `A`, true},
	{`a\tb`, "a\tb", true},
	{`(?:ab)+$`, `ababab`, true},
	{`^(?:ab)+$`, `ababa`, false},
	{`(?P<name>x)y`, `xy`, true},
	{`(a*)*$`, `b`, true},
	{`(a|b)*?c`, `ababc`, true},
	{`[]a]`, `]`, true},
	{`[a-]`, `-`, true},
	{`世界`, `こんにちは世界`, true},
}

func TestMatchString(t *testing.T) {
	for _, tt := range matchTests {
		re, err := Compile(tt.expr)
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.expr, err)
			continue
		}
		if got := re.MatchString(tt.s); got != tt.want {
			t.Errorf("Compile(%q).MatchString(%q) = %t, want %t", tt.expr, tt.s, got, tt.want)
		}
	}
}

type errorTest struct {
	expr string
	want string
}

// The expected errors are those of Go's regexp.Compile.
var errorTests = []errorTest{
	{`(`, "error parsing regexp: missing closing ): `(`"},
	{`a(b`, "error parsing regexp: missing closing ): `a(b`"},
	{`)`, "error parsing regexp: unexpected ): `)`"},
	{`a)b`, "error parsing regexp: unexpected ): `a)b`"},
	{`*`, "error parsing regexp: missing argument to repetition operator: `*`"},
	{`a|+`, "error parsing regexp: missing argument to repetition operator: `+`"},
	{`a**`, "error parsing regexp: invalid nested repetition operator: `**`"},
	{`[a`, "error parsing regexp: missing closing ]: `[a`"},
	{`x[^a`, "error parsing regexp: missing closing ]: `[^a`"},
	{`[z-a]`, "error parsing regexp: invalid character class range: `z-a`"},
	{`\q`, "error parsing regexp: invalid escape sequence: `\\q`"},
	{`\1`, "error parsing regexp: invalid escape sequence: `\\1`"},
	{`a\`, "error parsing regexp: trailing backslash at end of expression: ``"},
	{`a{1001}`, "error parsing regexp: invalid repeat count: `{1001}`"},
	{`a{2,1}`, "error parsing regexp: invalid repeat count: `{2,1}`"},
	{`(?z)`, "error parsing regexp: invalid or unsupported Perl syntax: `(?z`"},
}

func TestCompileErrors(t *testing.T) {
	for _, tt := range errorTests {
		_, err := Compile(tt.expr)
		if err == nil {
			t.Errorf("Compile(%q) succeeded, want %s", tt.expr, tt.want)
			continue
		}
		if got := err.Error(); got != tt.want {
			t.Errorf("Compile(%q): %s, want %s", tt.expr, got, tt.want)
		}
	}
}
