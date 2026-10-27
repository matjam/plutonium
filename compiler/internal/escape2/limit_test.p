// Ported from internal/escape2/limit_test.go at the go-cutover tag.

package escape2

import (
	"pluto/errors"
	"pluto/testing"

	"compiler/internal/diag"
	"compiler/internal/escape2/solve"
	"compiler/pu"
)

// TestRecoveredError checks that a solve stopped at its limits is reported as
// the program's error E3010, with the solver's message, and any other panic
// as an internal compiler error.
func TestRecoveredError(t *testing.T) {
	lim := pu.New(solve.LimitError{Func: "main.big", Bytes: 300 << 20})
	var d *diag.Diagnostic
	if err := recoveredError(lim); !errors.As(err, &d) || d.Code != diag.EscapeLimit || d.Msg != lim.Error() {
		t.Errorf("limit: got %v", err)
	}
	if err := recoveredError("boom"); !errors.As(err, &d) || d.Code != diag.InternalError {
		t.Errorf("other panic: got %v", err)
	}
}
