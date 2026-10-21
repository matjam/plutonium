package escape2

import (
	"errors"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/solve"
)

// TestRecoveredError checks that a solve stopped at its limits is reported as
// the program's error E3010, with the solver's message, and any other panic
// as an internal compiler error.
func TestRecoveredError(t *testing.T) {
	lim := &solve.LimitError{Func: "main.big", Bytes: 300 << 20}
	var d *diag.Diagnostic
	if err := recoveredError(lim); !errors.As(err, &d) || d.Code != diag.EscapeLimit || d.Msg != lim.Error() {
		t.Errorf("limit: got %v", err)
	}
	if err := recoveredError("boom"); !errors.As(err, &d) || d.Code != diag.InternalError {
		t.Errorf("other panic: got %v", err)
	}
}
