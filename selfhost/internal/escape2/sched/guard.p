// Translated by go2pu from internal/escape2/sched/guard.go at the go-cutover tag; edited by hand since.

package sched

// solvesWithoutPanic runs solve and reports whether it finished without a
// panic. The D44 cache's check solves against summaries loaded from a file:
// one the decoder accepted may still be one no solver makes, and a solve
// that fails on it means the check fails (the SCC is then solved from
// bottom). It is in a file of its own because the self-hosted compiler has a
// hand-written version: there a panic ends the task, an internal error
// either way (tools/go2pu).
func solvesWithoutPanic(solve func()) (ok bool) {
	/* go2pu: deferred recover dropped: a panic is not recovered in Plutonium */
	_ = ok
	solve()
	return true
}
