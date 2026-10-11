package parser

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/scanner"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestParseOrStmt checks the forms of the or statement (language spec
// 11.27) and the rule for the contextual keyword or (language spec 4.6):
// directly after the call that ends a simple statement it begins an or
// clause, and in every other position it is an identifier.
func TestParseOrStmt(t *testing.T) {
	testguard.Parallel(t)
	src := `package p

func f(path string, lines []string) (int, error) {
	load(path) or return
	cfg := load(path) or return fmt.Errorf("loading %v: %w", path, err)
	cfg = load(path) or return 0, err
	a, b := two() or return
	x.y, m[k] = two() or panic(err)
	n := strconv.Atoi(line) or continue
	n = strconv.Atoi(line) or break
	n = strconv.Atoi(line) or break Outer
	n = strconv.Atoi(line) or goto Fail
	f := os.Open(p) or panic(err)
	cfg := load(path) or {
		log.Printf("loading %v: %v", path, err)
		return err
	}
	v := Pick[int](xs) or return
	if v := strconv.Atoi(s) or return; v > 0 {
	}
	switch n := strconv.Atoi(line) or continue; {
	}
	for i := next() or break; i < 3; i++ {
	}
	if v := load(path) or { return T{1}, err }; v > 0 {
	}
	or := 3
	f(or)
	or.Close()
	or = or + 1
	x := or
}
`
	fset := token.NewFileSet()
	file, err := ParseFile(fset, "or.p", src, SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	body := file.Decls[0].(*ast.FuncDecl).Body.List
	type want struct {
		tok    token.Token // ILLEGAL for an or statement without a left side
		nlhs   int
		clause string // the clause's node type
	}
	wants := []want{
		{token.ILLEGAL, 0, "*ast.ReturnStmt"},
		{token.DEFINE, 1, "*ast.ReturnStmt"},
		{token.ASSIGN, 1, "*ast.ReturnStmt"},
		{token.DEFINE, 2, "*ast.ReturnStmt"},
		{token.ASSIGN, 2, "*ast.ExprStmt"},
		{token.DEFINE, 1, "*ast.BranchStmt"},
		{token.ASSIGN, 1, "*ast.BranchStmt"},
		{token.ASSIGN, 1, "*ast.BranchStmt"},
		{token.ASSIGN, 1, "*ast.BranchStmt"},
		{token.DEFINE, 1, "*ast.ExprStmt"},
		{token.DEFINE, 1, "*ast.BlockStmt"},
		{token.DEFINE, 1, "*ast.ReturnStmt"},
	}
	for i, w := range wants {
		or, ok := body[i].(*ast.OrStmt)
		if !ok {
			t.Errorf("statement %d is a %T, want an or statement", i, body[i])
			continue
		}
		if or.Tok != w.tok || len(or.Lhs) != w.nlhs {
			t.Errorf("statement %d: %s with %d operands, want %s with %d", i, or.Tok, len(or.Lhs), w.tok, w.nlhs)
		}
		if got := typeName(or.Clause); got != w.clause {
			t.Errorf("statement %d: clause is %s, want %s", i, got, w.clause)
		}
		if or.Tok == token.ILLEGAL && or.TokPos.IsValid() {
			t.Errorf("statement %d: TokPos set without a left side", i)
		}
		if or.End() != or.Clause.End() || or.Pos() >= or.Or {
			t.Errorf("statement %d: positions %d..%d, or at %d", i, or.Pos(), or.End(), or.Or)
		}
	}
	if r := body[1].(*ast.OrStmt).Clause.(*ast.ReturnStmt); len(r.Results) != 1 {
		t.Errorf("or return E: %d results, want 1", len(r.Results))
	}
	if r := body[2].(*ast.OrStmt).Clause.(*ast.ReturnStmt); len(r.Results) != 2 {
		t.Errorf("or return 0, err: %d results, want 2", len(r.Results))
	}
	if b := body[7].(*ast.OrStmt).Clause.(*ast.BranchStmt); b.Label == nil || b.Label.Name != "Outer" {
		t.Errorf("or break Outer: label %v", b.Label)
	}
	if b := body[8].(*ast.OrStmt).Clause.(*ast.BranchStmt); b.Tok != token.GOTO {
		t.Errorf("or goto Fail: %s", b.Tok)
	}
	if _, ok := body[11].(*ast.OrStmt).Call.Fun.(*ast.IndexExpr); !ok {
		t.Errorf("generic call: function is %T", body[11].(*ast.OrStmt).Call.Fun)
	}
	// The init statements of if, switch and for.
	inits := []ast.Stmt{
		body[12].(*ast.IfStmt).Init,
		body[13].(*ast.SwitchStmt).Init,
		body[14].(*ast.ForStmt).Init,
		body[15].(*ast.IfStmt).Init,
	}
	for i, s := range inits {
		if _, ok := s.(*ast.OrStmt); !ok {
			t.Errorf("init statement %d is a %T, want an or statement", i, s)
		}
	}
	if c := body[13].(*ast.SwitchStmt); c.Tag != nil {
		t.Errorf("switch with an or init statement has tag %v", c.Tag)
	}
	if c := body[14].(*ast.ForStmt); c.Cond == nil || c.Post == nil {
		t.Errorf("for with an or init statement lost its condition or post statement")
	}
	// A block clause in a header holds ordinary statements: T{1} is a
	// composite literal.
	blk := body[15].(*ast.IfStmt).Init.(*ast.OrStmt).Clause.(*ast.BlockStmt)
	if r := blk.List[0].(*ast.ReturnStmt); typeName(r.Results[0]) != "*ast.CompositeLit" {
		t.Errorf("block clause in an if header: %s", typeName(r.Results[0]))
	}
	// or as an identifier.
	idents := []string{"*ast.AssignStmt", "*ast.ExprStmt", "*ast.ExprStmt", "*ast.AssignStmt", "*ast.AssignStmt"}
	for i, w := range idents {
		if got := typeName(body[16+i]); got != w {
			t.Errorf("statement %d with the identifier or is a %s, want %s", 16+i, got, w)
		}
	}
	if id := body[16].(*ast.AssignStmt).Lhs[0].(*ast.Ident); id.Name != "or" || fset.Position(id.Pos()).Line != 27 {
		t.Errorf("or := 3: left side is %v at line %d", id, fset.Position(id.Pos()).Line)
	}
}

func typeName(n ast.Node) string {
	switch n.(type) {
	case *ast.ReturnStmt:
		return "*ast.ReturnStmt"
	case *ast.BranchStmt:
		return "*ast.BranchStmt"
	case *ast.BlockStmt:
		return "*ast.BlockStmt"
	case *ast.ExprStmt:
		return "*ast.ExprStmt"
	case *ast.AssignStmt:
		return "*ast.AssignStmt"
	case *ast.CompositeLit:
		return "*ast.CompositeLit"
	}
	return "other"
}

// TestParseOrErrors checks the syntax errors of the or statement: an
// operand that is not a call (E1013), a clause not on the line of the or,
// and or in a statement that takes no clause, where it is an identifier.
func TestParseOrErrors(t *testing.T) {
	testguard.Parallel(t)
	tests := []struct {
		src  string // the body of a function
		want string // the first error, without the file name
	}{
		{"a := g() + g() or return", "4:7: the operand of or is not a single call"},
		{"b := (f()) or return", "4:7: the operand of or is a call in parentheses; write the call without them"},
		{"c := f().x or return", "4:7: the operand of or is not a call; or must directly follow the call whose error it tests"},
		{"d := x or return", "4:7: the operand of or is not a single call"},
		{"a, b := x, f() or return", "4:10: the operand of or is a list of values; or follows a single call"},
		{"e := (x) or return", "4:7: the operand of or is not a single call"},
		{"f() or\n\treturn", "4:6: or must be followed by its clause on the same line"},
		{"f() or; return", "4:8: or must be followed by a clause: a block, a return, break, continue or goto statement, or a call such as panic(err)"},
		{"f()\n\tor return", "5:2: or must be on the line where its call ends: a newline after the call ends the statement"},
		{"f()\n\tor panic(err)", "5:2: or must be on the line where its call ends: a newline after the call ends the statement"},
		{"var x = f() or return", "4:14: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"defer f() or return", "4:12: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"task f() or return", "4:11: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"n += f() or return", "4:11: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"ch <- f() or return", "4:12: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"for v := range f() or return {\n\t}", "4:21: expected '{', found or"},
		{"select {\n\tcase v := <-ch or return:\n\t}", "5:17: expected ':', found or"},
	}
	for _, tt := range tests {
		src := "package p\n\nfunc h() {\n\t" + tt.src + "\n}\n"
		_, err := ParseFile(token.NewFileSet(), "e.p", src, SkipObjectResolution)
		if err == nil {
			t.Errorf("%q: no error", tt.src)
			continue
		}
		got := err.(scanner.ErrorList)[0].Error()
		if got = strings.TrimPrefix(got, "e.p:"); got != tt.want {
			t.Errorf("%q:\n got %s\nwant %s", tt.src, got, tt.want)
		}
	}
}
