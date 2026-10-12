package flag

import (
	"pluto/errors"
	"pluto/strings"
	"pluto/testing"
	"pluto/time"
)

// The expected behaviour and output come from Go's package flag.

var sv string

func TestParse(t *testing.T) {
	fs := NewFlagSet("test", ContinueOnError)
	defer fs.Drop()
	b := fs.Bool("bool", false, "bool value")
	b2 := fs.Bool("bool2", false, "bool2 value")
	i := fs.Int("int", 0, "int value")
	i64 := fs.Int64("int64", 0, "int64 value")
	u := fs.Uint("uint", 0, "uint value")
	u64 := fs.Uint64("uint64", 0, "uint64 value")
	s := fs.String("string", "0", "string value")
	d := fs.Duration("duration", 5*time.Second, "time.Duration value")
	// The FlagSet keeps &sv in memory it allocated, which the escape
	// analysis cannot tell is released before this frame ends, so sv is
	// a package variable (language spec section 18.3).
	fs.StringVar(&sv, "sv", "x", "a string variable")
	args := []string{
		"-bool", "-bool2=true", "--int", "22", "--int64", "0x23", "-uint", "24",
		"--uint64", "25", "-string", "hello", "-duration=2m", "-sv=", "extra", "-int=1", "one more",
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if !fs.Parsed() || !*b || !*b2 || *i != 22 || *i64 != 0x23 || *u != 24 || *u64 != 25 || *s != "hello" ||
		*d != 2*time.Minute || sv != "" {
		t.Errorf("values: %v %v %d %d %d %d %q %d %q", *b, *b2, *i, *i64, *u, *u64, *s, int64(*d), sv)
	}
	if fs.NArg() != 3 || fs.Arg(0) != "extra" || fs.Arg(1) != "-int=1" || fs.Arg(2) != "one more" || fs.Arg(3) != "" {
		t.Errorf("args: %d %q", fs.NArg(), fs.Arg(0))
	}
	if fs.NFlag() != 9 {
		t.Errorf("NFlag = %d, want 9", fs.NFlag())
	}
	if fl := fs.Lookup("duration"); fl == nil || fl.DefValue != "5s" || fl.Value.String() != "2m0s" {
		t.Error("Lookup duration")
	}
	if fl := fs.Lookup("int64"); fl.Value.String() != "35" || fl.DefValue != "0" {
		t.Error("Lookup int64")
	}
	// Visit sees the set flags in order; VisitAll every flag.
	var names strings.Builder
	fs.Visit(func(f *Flag) {
		names.WriteString(f.Name)
		names.WriteByte(' ')
	})
	if names.String() != "bool bool2 duration int int64 string sv uint uint64 " {
		t.Errorf("Visit: %q", names.String())
	}
	names.Free()
	n := 0
	fs.VisitAll(func(f *Flag) { n++ })
	if n != 9 {
		t.Errorf("VisitAll: %d", n)
	}
}

func TestParseTerminators(t *testing.T) {
	fs := NewFlagSet("t", ContinueOnError)
	defer fs.Drop()
	x := fs.Bool("x", false, "")
	if err := fs.Parse([]string{"-x", "--", "-x"}); err != nil || !*x || fs.NArg() != 1 || fs.Arg(0) != "-x" {
		t.Errorf("--: %v %d", err, fs.NArg())
	}
	fs2 := NewFlagSet("t", ContinueOnError)
	defer fs2.Drop()
	fs2.Bool("x", false, "")
	if err := fs2.Parse([]string{"-", "-x"}); err != nil || fs2.NArg() != 2 {
		t.Errorf("-: %v %d", err, fs2.NArg())
	}
}

func TestErrors(t *testing.T) {
	tests := [...]struct {
		args []string
		want string
	}{
		{[]string{"-undefined"}, "flag provided but not defined: -undefined"},
		{[]string{"-int"}, "flag needs an argument: -int"},
		{[]string{"-int=x"}, "invalid value \"x\" for flag -int: parse error"},
		{[]string{"-int=99999999999999999999"}, "invalid value \"99999999999999999999\" for flag -int: value out of range"},
		{[]string{"-bool=maybe"}, "invalid boolean value \"maybe\" for -bool: parse error"},
		{[]string{"---x"}, "bad flag syntax: ---x"},
		{[]string{"-=x"}, "bad flag syntax: -=x"},
		{[]string{"-dur=3"}, "invalid value \"3\" for flag -dur: parse error"},
	}
	for _, tt := range tests {
		var out strings.Builder
		fs := NewFlagSet("e", ContinueOnError)
		fs.SetOutput(&out)
		fs.Int("int", 0, "")
		fs.Bool("bool", false, "")
		fs.Duration("dur", 0, "")
		err := fs.Parse(tt.args)
		if err == nil || err.Error() != tt.want {
			t.Errorf("Parse(%q): %v, want %q", tt.args[0], err, tt.want)
		} else if o := out.String(); !strings.HasPrefix(o, tt.want) || !strings.HasPrefix(o[len(tt.want):], "\nUsage of e:\n") {
			t.Errorf("output %q", out.String())
		}
		err = nil
		out.Free()
		fs.Drop()
	}
	var out strings.Builder
	fs := NewFlagSet("h", ContinueOnError)
	fs.SetOutput(&out)
	called := false
	fs.Usage = func() { called = true }
	if err := fs.Parse([]string{"-help"}); err != ErrHelp || !called {
		t.Errorf("-help: %v %v", err, called)
	}
	fs.Drop()
	out.Free()
}

func TestSetAndPanics(t *testing.T) {
	fs := NewFlagSet("s", ContinueOnError)
	defer fs.Drop()
	n := fs.Int("n", 1, "")
	if err := fs.Set("n", "7"); err != nil || *n != 7 || fs.NFlag() != 1 {
		t.Errorf("Set: %v %d", err, *n)
	}
	err := fs.Set("missing", "1")
	if err == nil || err.Error() != "no such flag -missing" {
		t.Errorf("Set missing: %v", err)
	}
	err = nil
}

func TestPrintDefaults(t *testing.T) {
	var out strings.Builder
	fs := NewFlagSet("myName", ContinueOnError)
	defer fs.Drop()
	fs.SetOutput(&out)
	fs.Bool("A", false, "for bootstrapping, allow 'any' type")
	fs.Bool("Alongflagname", false, "disable bounds checking")
	fs.Bool("C", true, "a boolean defaulting to true")
	fs.String("D", "", "set relative `path` for local imports")
	fs.String("E", "0", "issue 23543")
	fs.String("M", "", "a multiline\nhelp\nstring")
	fs.Int("N", 27, "a non-zero int")
	fs.Bool("O", true, "a flag\nmultiline help string")
	fs.Int("Z", 0, "an int that defaults to zero")
	fs.Duration("maxT", 0, "set `timeout` for dial")
	fs.Uint("U", 3, "a uint")
	fs.Duration("dur", 90*time.Second, "a duration")
	fs.PrintDefaults()
	want := "  -A\tfor bootstrapping, allow 'any' type\n" +
		"  -Alongflagname\n    \tdisable bounds checking\n" +
		"  -C\ta boolean defaulting to true (default true)\n" +
		"  -D path\n    \tset relative path for local imports\n" +
		"  -E string\n    \tissue 23543 (default \"0\")\n" +
		"  -M string\n    \ta multiline\n    \thelp\n    \tstring\n" +
		"  -N int\n    \ta non-zero int (default 27)\n" +
		"  -O\ta flag\n    \tmultiline help string (default true)\n" +
		"  -U uint\n    \ta uint (default 3)\n" +
		"  -Z int\n    \tan int that defaults to zero\n" +
		"  -dur duration\n    \ta duration (default 1m30s)\n" +
		"  -maxT timeout\n    \tset timeout for dial\n"
	if out.String() != want {
		t.Errorf("PrintDefaults:\n%s\nwant:\n%s", out.String(), want)
	}
	out.Free()
}

// A flag name longer than any line buffer prints whole.
func TestPrintDefaultsLongName(t *testing.T) {
	var out strings.Builder
	defer out.Free()
	fs := NewFlagSet("long", ContinueOnError)
	defer fs.Drop()
	fs.SetOutput(&out)
	name := strings.Repeat("n", 300)
	fs.String(name, "", "a long `value`")
	fs.PrintDefaults()
	want := "  -" + name + " value\n    \ta long value\n"
	if out.String() != want {
		t.Errorf("PrintDefaults with a long name: %d bytes, want %d", out.Len(), len(want))
	}
}
