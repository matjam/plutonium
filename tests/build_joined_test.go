// Ported from internal/build/joined_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// Decision D31 and section 15.4: package cleanup and the exact leak report
// are guaranteed only when every task has retired when main returns, which a
// sync.Scope join provides. A sync.WaitGroup, a channel or an atomic counter
// releases main while the task that signalled it is still returning, and
// main may then see the task as running: the program takes the abandon path,
// ends with main's status, skips package cleanup and, under the debug
// allocator, prints one line naming the tasks still running instead of the
// leak report. After the short wait the runtime allows for it, this is rare,
// but on a machine under load it happens, so a test that joins its tasks that
// way and checks the exact report must not fail on it.
//
// What these tests check is what the decision guarantees: a run that does
// not take the abandon path accounts exactly (it must produce the expected
// output and status, and no leak), and a program that does not take it in
// joinAttempts runs in a row fails. A run that takes the abandon path is
// repeated, never accepted as a pass without the report. (joinAttempts and
// abandonMark are in programs_test.go, with runDebugAlloc.)

// tookAbandonPath reports whether the output of a run under the debug
// allocator shows that main returned with tasks still running.
func tookAbandonPath(out string) bool { return strings.Contains(out, abandonMark) }

// untilRetired runs a program joined without a sync.Scope until a run does
// not take the abandon path, and returns that run's output and status.
// abandoned says whether a run did (it receives the output and status). The
// test fails if joinAttempts runs in a row did.
func untilRetired(t *testing.T, run func() (string, int), abandoned func(out string, status int) bool) (string, int) {
	t.Helper()
	var out string
	var status int
	for range joinAttempts {
		out, status = run()
		if !abandoned(out, status) {
			return out, status
		}
	}
	t.Fatalf("%d runs in a row took the abandon path of section 15.4 (the tasks never retired); last output %q, status %d", joinAttempts, out, status)
	return out, status
}

// A run that took the abandon path is run again, and the run that did not is
// the one returned.
func TestUntilRetired(t *testing.T) {
	calls := 0
	out, status := untilRetired(t, func() (string, int) {
		calls++
		if calls < 4 {
			return "debug allocator: 1 task " + abandonMark + "\n", 0
		}
		return "ok\n", 0
	}, func(out string, _ int) bool { return tookAbandonPath(out) })
	if out != "ok\n" || status != 0 || calls != 4 {
		t.Errorf("got %q, status %d after %d runs; want ok after 4", out, status, calls)
	}
	if tookAbandonPath("ok\n") || !tookAbandonPath("debug allocator: 2 tasks "+abandonMark+"\n") {
		t.Error("tookAbandonPath does not recognise the debug allocator's line")
	}
}
