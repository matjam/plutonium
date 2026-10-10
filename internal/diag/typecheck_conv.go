package diag

import (
	"fmt"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// The conversions of Go that Plutonium removes (language spec section 3):
// each fix is written with the conversion's own argument.

// conversionAt returns the conversion call whose type name is at the
// error's position.
func (s *spec) conversionAt() *ast.CallExpr {
	for _, n := range s.path {
		if c, ok := n.(*ast.CallExpr); ok && c.Fun.Pos() == s.pos && len(c.Args) == 1 {
			return c
		}
	}
	return nil
}

// keepText makes the catalog's explanation the diagnostic's own, so that
// the fixes a specialiser sets are printed with it.
func (s *spec) keepText() {
	if e := Lookup(s.d.Code); e != nil && s.d.Text == "" {
		s.d.Text = e.Text
	}
}

// resultName returns the name of the variable that receives call, if call
// is the whole right-hand side of an assignment to a name, and fallback
// otherwise.
func (s *spec) resultName(call *ast.CallExpr, fallback string) string {
	if as, ok := find[*ast.AssignStmt](s); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 && as.Rhs[0] == ast.Expr(call) {
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			return id.Name
		}
	}
	return fallback
}

// isReturned reports whether call is the only value a return statement
// gives.
func (s *spec) isReturned(call *ast.CallExpr) bool {
	ret, ok := find[*ast.ReturnStmt](s)
	return ok && len(ret.Results) == 1 && ret.Results[0] == ast.Expr(call)
}

// explainBytesToString writes the fixes for string(b): strings.FromUTF8 to
// check the bytes, and strings.FromUTF8Lossy to replace the ones that are
// not valid.
func explainBytesToString(s *spec) {
	call := s.conversionAt()
	if call == nil {
		return
	}
	s.keepText()
	b := types2.ExprString(call.Args[0])
	name := s.resultName(call, "s")
	const lossyIntro = "To replace each byte that is not valid UTF-8 with U+FFFD, as string(b) did"
	if s.isReturned(call) {
		s.d.Fix = &Fix{Intro: "To fail on bytes that are not UTF-8, return the error",
			Code: fmt.Sprintf("import \"pluto/strings\"\n\ns, err := strings.FromUTF8(%s)\nif err != nil {\n\treturn \"\", err\n}\nreturn s, nil", b)}
		s.d.More = []*Fix{{Intro: lossyIntro,
			Code: fmt.Sprintf("import \"pluto/strings\"\n\nreturn strings.FromUTF8Lossy(%s)", b)}}
		return
	}
	s.d.Fix = &Fix{Intro: "To fail on bytes that are not UTF-8, check them and handle the error",
		Code: fmt.Sprintf("import \"pluto/strings\"\n\n%s, err := strings.FromUTF8(%s)\nif err != nil {\n\t// the bytes are not text\n}", name, b)}
	lossy := fmt.Sprintf("strings.FromUTF8Lossy(%s)", b)
	if s.resultName(call, "") != "" {
		lossy = name + " := " + lossy
	}
	s.d.More = []*Fix{{Intro: lossyIntro, Code: "import \"pluto/strings\"\n\n" + lossy}}
}
