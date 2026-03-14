package scanner_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/scanner"
	"github.com/matjam/plutonium/internal/token"
)

func scan(src string) []token.Token {
	fset := token.NewFileSet()
	file := fset.AddFile("test.pu", -1, len(src))
	var s scanner.Scanner
	var toks []token.Token
	s.Init(file, []byte(src), nil)
	for {
		_, tok, _ := s.Scan()
		toks = append(toks, tok)
		if tok == token.EOF {
			break
		}
	}
	return toks
}

func TestScanBasic(t *testing.T) {
	toks := scan("package main")
	want := []token.Token{token.PACKAGE, token.IDENT, token.SEMICOLON, token.EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %v, want %v", toks, want)
	}
	for i, tok := range toks {
		if tok != want[i] {
			t.Errorf("tok[%d]: got %v, want %v", i, tok, want[i])
		}
	}
}

func TestScanIntegers(t *testing.T) {
	toks := scan("42 0xFF 0b1010 0o777")
	for i, tok := range toks {
		if tok == token.EOF || tok == token.SEMICOLON {
			break
		}
		if tok != token.INT {
			t.Errorf("tok[%d]: got %v, want INT", i, tok)
		}
	}
}

func TestScanString(t *testing.T) {
	toks := scan(`"hello world"`)
	if toks[0] != token.STRING {
		t.Errorf("got %v, want STRING", toks[0])
	}
}

func TestScanRawString(t *testing.T) {
	toks := scan("`hello world`")
	if toks[0] != token.STRING {
		t.Errorf("got %v, want STRING", toks[0])
	}
}

func TestScanForbiddenKeywords(t *testing.T) {
	for _, kw := range []string{"go", "chan", "select", "map"} {
		toks := scan(kw)
		tok := toks[0]
		if !tok.IsForbidden() {
			t.Errorf("keyword %q: expected forbidden token, got %v", kw, tok)
		}
	}
}

func TestScanDirective(t *testing.T) {
	src := "//pu:build amd64\npackage main"
	fset := token.NewFileSet()
	file := fset.AddFile("test.pu", -1, len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil)
	for {
		_, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
	}
	dirs := s.Directives()
	if len(dirs) == 0 {
		t.Error("expected at least one //pu: directive")
	}
	if dirs[0].Text != "build amd64" {
		t.Errorf("got directive text %q, want %q", dirs[0].Text, "build amd64")
	}
}

func TestScanOperators(t *testing.T) {
	cases := []struct {
		src  string
		want token.Token
	}{
		{"+", token.ADD},
		{"-", token.SUB},
		{"*", token.MUL},
		{"/", token.QUO},
		{"%", token.REM},
		{"&&", token.LAND},
		{"||", token.LOR},
		{":=", token.DEFINE},
		{"...", token.ELLIPSIS},
		{"<<", token.SHL},
		{">>", token.SHR},
		{"&^", token.AND_NOT},
	}
	for _, c := range cases {
		toks := scan(c.src)
		if toks[0] != c.want {
			t.Errorf("scan(%q): got %v, want %v", c.src, toks[0], c.want)
		}
	}
}

func TestScanFloat(t *testing.T) {
	toks := scan("3.14 1e10 2.5e-3")
	for i, tok := range toks {
		if tok == token.EOF || tok == token.SEMICOLON {
			break
		}
		if tok != token.FLOAT {
			t.Errorf("tok[%d]: got %v, want FLOAT", i, tok)
		}
	}
}

func TestScanSemicolonInsertion(t *testing.T) {
	// After ident, int, float, char, string, ), ], } — semicolon is auto-inserted
	toks := scan("x\ny")
	// expect: IDENT SEMICOLON IDENT SEMICOLON EOF
	if len(toks) < 3 {
		t.Fatalf("too few tokens: %v", toks)
	}
	if toks[1] != token.SEMICOLON {
		t.Errorf("expected SEMICOLON after ident, got %v", toks[1])
	}
}

func TestScanErrorHandler(t *testing.T) {
	var errCount int
	fset := token.NewFileSet()
	file := fset.AddFile("test.pu", -1, 10)
	var s scanner.Scanner
	s.Init(file, []byte("@"), func(pos token.Pos, msg string) {
		errCount++
	})
	s.Scan()
	if errCount == 0 {
		t.Error("expected error for illegal character @")
	}
}
