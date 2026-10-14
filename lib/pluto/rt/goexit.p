package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Goexit (language spec section 17.3). Goexit ends the calling task: every
// frame on it runs its deferred calls and releases what it holds, innermost
// first, as if each returned, and then the task ends. It is not an
// unwinder: Goexit sets the task's goexit flag and returns, and the
// compiler checks the flag after every call that may end this way
// (internal/lower/goexit.go). A frame that finds it set clears it
// (takeGoexit), leaves through its ordinary return path and sets it again
// for its caller (setGoexit). At the bottom of a task started by a task
// statement, taskMain ends the task as if its function had returned; at
// the bottom of the main task, pluto.main calls mainGoexit; CatchGoexit
// stops the exit for a test runner.

// Goexit ends the calling task after running the deferred calls and
// releasing the variables of every function on its stack, innermost first,
// as Go's runtime.Goexit does. Other tasks are not affected. Called from
// the task that runs main, it ends that task: main does not return, and
// the program goes on until every other task has ended or waits for good,
// and then stops with "fatal error: no tasks (main called rt.Goexit)
// - deadlock!" and status 2. A deferred call that calls Goexit does not
// stop the deferred calls after it.
func Goexit() {
	t := current()
	if t == nil {
		panicMessage("rt.Goexit called outside a task", "", "", "", "")
	}
	t.goexit = true
}

// takeGoexit reports whether the running task is leaving its frames for
// Goexit, and clears the flag: the caller leaves, running its deferred
// calls with the flag clear, and then sets it again with setGoexit.
func takeGoexit() bool {
	t := current()
	if t == nil || !t.goexit {
		return false
	}
	t.goexit = false
	return true
}

// setGoexit sets the running task's Goexit flag for its caller.
func setGoexit() {
	if t := current(); t != nil {
		t.goexit = true
	}
}

// CatchGoexit calls f and reports whether f ended with Goexit. If it did,
// the exit stops here, once every frame of f has run its deferred calls and
// released its variables, and the task goes on. A test runner uses it, so
// that FailNow ends the test function and not the test binary
// (pluto/testing).
func CatchGoexit(f func()) bool {
	f()
	return takeGoexit()
}

// mainGoexited is set once main has called Goexit.
var mainGoexited bool

// mainGoexit ends the main task after it called Goexit and its frames have
// run their deferred calls. Without other tasks the program stops at once;
// otherwise it waits, and the scheduler stops it once nothing can run.
func mainGoexit() {
	mainGoexited = true
	if !tasking {
		noTasksLeft()
	}
	for {
		park(WaitSystem)
	}
}

// noTasksLeft stops the program after main called Goexit and no other
// task can run.
func noTasksLeft() {
	const msg = "fatal error: no tasks (main called rt.Goexit) - deadlock!\n"
	consoleWrite(unsafe.Pointer(unsafe.StringData(msg)), len(msg))
	exit(2)
}

// goexitFromC stops the program when Goexit would leave a function that C
// code called (a //pu:export wrapper): C frames cannot be left this way.
func goexitFromC() {
	panicMessage("rt.Goexit cannot leave a function called from C", "", "", "", "")
}
