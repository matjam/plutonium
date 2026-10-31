// Package typesprobe is a cheap check that the escape analysis accepts
// types2 in a test build with generation checks (TestCompilerUnits runs it
// with the light units, in minutes). The heavy escape2 test builds link
// types2 too, but take an hour.
//
// It caught three false rejections, each of a pointer the analysis took to
// escape through a call through an interface (spec 18.3):
//   - conversions.p, "cannot pass &cause to (*operand).convertibleTo": a
//     cut of a deep path stood for the pointer loaded from it;
//   - testing.p, "cannot pass &buf to sprintln": a join kept one of the
//     two narrowings of a key (its receiver's type and a pointer's type
//     further down), and lost the other;
//   - (*operand).convertibleTo over MaxFuncArena (E3010): a chain of
//     conjunct filters, one per call through an interface on the way.
package typesprobe

import (
	"pluto/testing"

	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/summary"
	"compiler/internal/types2"
)

var conf types2.Config

// TestCheck type-checks an empty package and widens an empty summary, so
// that the test links types2's checker and escape2's summaries.
func TestCheck(t *testing.T) {
	if _, err := conf.Check("p", nil, nil, nil); err != nil {
		t.Fatalf("Check of an empty package: %v", err)
	}
	s := &summary.Summary{Inputs: 1}
	summary.Widen(domain.NewPaths(), s)
	if len(s.Esc) != 0 {
		t.Fatalf("Widen of an empty summary made %d keys", len(s.Esc))
	}
}
