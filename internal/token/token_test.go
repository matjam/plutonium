package token_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/token"
)

func TestTokenStrings(t *testing.T) {
	cases := []struct {
		tok  token.Token
		want string
	}{
		{token.ILLEGAL, "ILLEGAL"},
		{token.EOF, "EOF"},
		{token.IDENT, "IDENT"},
		{token.INT, "INT"},
		{token.FUNC, "func"},
		{token.RETURN, "return"},
		{token.IF, "if"},
		{token.FOR, "for"},
		{token.PACKAGE, "package"},
		{token.IMPORT, "import"},
	}
	for _, c := range cases {
		if got := c.tok.String(); got != c.want {
			t.Errorf("token %d: got %q, want %q", c.tok, got, c.want)
		}
	}
}

func TestForbiddenTokens(t *testing.T) {
	forbidden := []token.Token{
		token.GO,
		token.CHAN,
		token.SELECT,
		token.MAP,
	}
	for _, tok := range forbidden {
		if tok.String() == "" {
			t.Errorf("forbidden token %d has empty string", tok)
		}
		if !tok.IsForbidden() {
			t.Errorf("token %q should be forbidden", tok)
		}
	}
}

func TestKeywordLookup(t *testing.T) {
	cases := []struct {
		ident string
		want  token.Token
	}{
		{"func", token.FUNC},
		{"return", token.RETURN},
		{"if", token.IF},
		{"else", token.ELSE},
		{"for", token.FOR},
		{"range", token.RANGE},
		{"package", token.PACKAGE},
		{"import", token.IMPORT},
		{"var", token.VAR},
		{"const", token.CONST},
		{"type", token.TYPE},
		{"struct", token.STRUCT},
		{"interface", token.INTERFACE},
		{"go", token.GO},
		{"chan", token.CHAN},
		{"select", token.SELECT},
		{"map", token.MAP},
		{"notakeyword", token.IDENT},
	}
	for _, c := range cases {
		if got := token.Lookup(c.ident); got != c.want {
			t.Errorf("Lookup(%q): got %v, want %v", c.ident, got, c.want)
		}
	}
}

func TestPositionValid(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("test.pu", -1, 100)
	pos := f.Pos(0)
	if !pos.IsValid() {
		t.Error("expected valid pos")
	}
	p := fset.Position(pos)
	if p.Filename != "test.pu" {
		t.Errorf("got filename %q, want %q", p.Filename, "test.pu")
	}
	if p.Line != 1 {
		t.Errorf("got line %d, want 1", p.Line)
	}
}

func TestNoPos(t *testing.T) {
	if token.NoPos.IsValid() {
		t.Error("NoPos should not be valid")
	}
}
