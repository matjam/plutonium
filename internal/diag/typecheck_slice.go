package diag

import (
	"fmt"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// sliceAt returns the slice expression that starts at the error's position.
func (s *spec) sliceAt() *ast.SliceExpr {
	for _, n := range s.path {
		if e, ok := n.(*ast.SliceExpr); ok && e.Pos() == s.pos {
			return e
		}
	}
	return nil
}

// explainStringSlice writes the fix for a slice expression on a string with
// the expression's own operand and bounds: the strings.Slice or
// strings.SliceFrom call that takes the same substring, and for s[:n] the
// strings.Truncate call that rounds down instead of panicking (language
// spec section 11.12).
func explainStringSlice(s *spec) {
	e := s.sliceAt()
	if e == nil {
		return
	}
	s.keepText()
	str := types2.ExprString(e.X)
	lo, hi := "0", ""
	if e.Low != nil {
		lo = types2.ExprString(e.Low)
	}
	if e.High != nil {
		hi = types2.ExprString(e.High)
	}
	const imp = "import \"pluto/strings\"\n\n"
	switch {
	case e.Low == nil && e.High == nil:
		s.d.Fix = &Fix{Intro: "A string cannot change, so the whole string is the same value", Code: str}
	case e.High == nil:
		s.d.Fix = &Fix{Intro: "To take the rest of the string, which panics unless the bound is on a rune boundary",
			Code: fmt.Sprintf("%ssub := strings.SliceFrom(%s, %s)", imp, str, lo)}
	default:
		s.d.Fix = &Fix{Intro: "To take a substring, which panics unless both bounds are on rune boundaries",
			Code: fmt.Sprintf("%ssub := strings.Slice(%s, %s, %s)", imp, str, lo, hi)}
		if e.Low == nil {
			s.d.More = []*Fix{{Intro: "To take at most that many bytes, ending on a rune boundary instead of panicking",
				Code: fmt.Sprintf("%ssub := strings.Truncate(%s, %s)", imp, str, hi)}}
		}
	}
}
