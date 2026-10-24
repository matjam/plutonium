package escape2

import "selfhost/internal/escape2/sched"

// recoverInternal is the Go compiler's guard (internal/escape2/guard.go),
// which turns a crash of the analysis into an internal compiler error with
// recover. Plutonium has no recover: a panic ends the task, an internal
// error either way. Hand-written; go2pu leaves guard.go out.
func recoverInternal(res **sched.Result, err *error) {
	_, _ = res, err
}
