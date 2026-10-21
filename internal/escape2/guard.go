package escape2

import "github.com/matjam/plutonium/internal/escape2/sched"

// recoverInternal turns a panic of the analysis into an internal compiler
// error (CheckWith), never acceptance. It is in a file of its own because
// the self-hosted compiler has a hand-written version: there a panic ends
// the task, which is an internal error either way (tools/go2pu).
func recoverInternal(res **sched.Result, err *error) {
	if r := recover(); r != nil {
		*res, *err = nil, recoveredError(r)
	}
}
