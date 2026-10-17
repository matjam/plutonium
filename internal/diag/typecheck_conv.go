package diag

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// The conversions of Go that Plutonium removes (language spec section 11.24):
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

// enclosingCall returns the call of the built-in or function name that has
// call as its only argument, or nil.
func (s *spec) enclosingCall(call *ast.CallExpr, name string) *ast.CallExpr {
	for _, n := range s.path {
		if c, ok := n.(*ast.CallExpr); ok && c != call && len(c.Args) == 1 && c.Args[0] == ast.Expr(call) {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == name {
				return c
			}
		}
	}
	return nil
}

// rangedOver reports whether call is the expression a for statement ranges
// over.
func (s *spec) rangedOver(call *ast.CallExpr) bool {
	r, ok := find[*ast.RangeStmt](s)
	return ok && r.X == ast.Expr(call)
}

// explainStringToSlice writes the fixes for []byte(s) and []rune(s) with the
// conversion's own argument, and the one-line replacement for the idioms
// that never needed the copy: len([]rune(s)) and a range over []byte(s) or
// []rune(s).
func explainStringToSlice(s *spec) {
	call := s.conversionAt()
	if call == nil {
		return
	}
	s.keepText()
	str := types2.ExprString(call.Args[0])
	runes := strings.HasPrefix(s.first, "[]rune")
	if runes && s.enclosingCall(call, "len") != nil {
		s.d.Fix = &Fix{Intro: "To count the runes of a string, count them without copying",
			Code: fmt.Sprintf("import \"pluto/unicode/utf8\"\n\nn := utf8.RuneCountInString(%s)", str)}
		return
	}
	if runes && s.rangedOver(call) {
		s.d.Fix = &Fix{Intro: "To visit the runes of a string, range over the string; i is the byte offset of r",
			Code: fmt.Sprintf("for i, r := range %s {\n\t...\n}", str)}
		return
	}
	if !runes && s.rangedOver(call) {
		s.d.Fix = &Fix{Intro: "To visit the bytes of a string, index it",
			Code: fmt.Sprintf("for i := 0; i < len(%s); i++ {\n\tb := %s[i]\n\t...\n}", str, str)}
		return
	}
	if runes {
		name := s.resultName(call, "r")
		s.d.Fix = &Fix{Intro: "To visit the runes of a string, range over the string; i is the byte offset of r",
			Code: fmt.Sprintf("for i, r := range %s {\n\t...\n}", str)}
		s.d.More = []*Fix{{Intro: "To keep the runes in a vec.Vec[rune] that you own, which frees them when it is dropped",
			Code: fmt.Sprintf("import \"pluto/strings\"\n\n%s := strings.Runes(%s)\n// %s.Slice() is the []rune", name, str, name)}}
		return
	}
	name := s.resultName(call, "b")
	s.d.Fix = &Fix{Intro: "To keep a copy that you own, in a vec.Vec[byte] that frees it when it is dropped",
		Code: fmt.Sprintf("import \"pluto/strings\"\n\n%s := strings.Bytes(%s)\n// %s.Slice() is the []byte", name, str, name)}
	s.d.More = []*Fix{{Intro: "To copy into a buffer of your own without allocating",
		Code: fmt.Sprintf("var buf [64]byte\nn := copy(buf[:], %s)\n%s := buf[:n]", str, name)}}
}

// explainIntToString writes the two fixes for string(i): the character with
// code i, and the digits of i, with the formatting function that takes i's
// type.
func explainIntToString(s *spec) {
	call := s.conversionAt()
	if call == nil {
		return
	}
	s.keepText()
	x := types2.ExprString(call.Args[0])
	if T := s.typeOf(call.Args[0]); T != nil {
		if _, ok := T.Underlying().(*types2.Slice); ok {
			s.d.Fix = &Fix{Intro: "To write the characters whose codes are the elements, convert each one to a rune",
				Code: fmt.Sprintf("import \"pluto/strings\"\n\nvar b strings.Builder\nfor _, c := range %s {\n\tb.WriteRune(rune(c))\n}\ntext := b.String()", x)}
			return
		}
	}
	s.d.Fix = &Fix{Intro: fmt.Sprintf("To make the character whose code is %s, convert to a rune first", x),
		Code: fmt.Sprintf("string(rune(%s))", x)}
	T := s.typeOf(call.Args[0])
	var digits string
	switch {
	case T != nil && types2.Identical(T, types2.Typ[types2.Int]):
		digits = fmt.Sprintf("strconv.Itoa(%s)", x)
	case T != nil && types2.Identical(T, types2.Typ[types2.Int64]):
		digits = fmt.Sprintf("strconv.FormatInt(%s, 10)", x)
	case T != nil && types2.Identical(T, types2.Typ[types2.Uint64]):
		digits = fmt.Sprintf("strconv.FormatUint(%s, 10)", x)
	case isKind(T, types2.IsUnsigned):
		digits = fmt.Sprintf("strconv.FormatUint(uint64(%s), 10)", x)
	default:
		digits = fmt.Sprintf("strconv.FormatInt(int64(%s), 10)", x)
	}
	s.d.More = []*Fix{{Intro: fmt.Sprintf("To make the digits of %s, as text", x),
		Code: "import \"pluto/strconv\"\n\n" + digits}}
}
