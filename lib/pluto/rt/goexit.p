package rt

import "unsafe"

// ----------------------------------------------------------------------------
// ExitTask (language spec section 17.3). ExitTask ends the calling task: every
// frame on it runs its deferred calls and releases what it holds, innermost
// first, as if each returned, and then the task ends. It is not an
// unwinder: ExitTask sets the task's goexit flag and returns, and the
// compiler checks the flag after every call that may end this way
// (internal/lower/goexit.go). A frame that finds it set clears it
// (takeGoexit), leaves through its ordinary return path and sets it again
// for its caller (setGoexit). At the bottom of a task started by a task
// statement, taskMain ends the task as if its function had returned; at
// the bottom of the main task, pluto.main calls mainGoexit; CatchExitTask
// stops the exit for a test runner.

// ExitTask ends the calling task after running the deferred calls and
// releasing the variables of every function on its stack, innermost first.
// Other tasks are not affected. Called from the task that runs main, it ends
// that task: main does not return, and
// the program goes on until every other task has ended or waits for good,
// and then stops with "fatal error: no tasks (main called rt.ExitTask)
// - deadlock!" and status 2. A deferred call that calls ExitTask does not
// stop the deferred calls after it.
func ExitTask() {
	t := current()
	if t == nil {
		panicMessage("rt.ExitTask called outside a task", "", "", "", "")
	}
	t.goexit = true
}

// takeGoexit reports whether the running task is leaving its frames for
// ExitTask, and clears the flag: the caller leaves, running its deferred
// calls with the flag clear, and then sets it again with setGoexit.
func takeGoexit() bool {
	t := current()
	if t == nil || !t.goexit {
		return false
	}
	t.goexit = false
	return true
}

// setGoexit sets the running task's ExitTask flag for its caller.
func setGoexit() {
	if t := current(); t != nil {
		t.goexit = true
	}
}

// CatchExitTask calls f and reports whether f ended with ExitTask. If it did,
// the exit stops here, once every frame of f has run its deferred calls and
// released its variables, and the task goes on. A test runner uses it, so
// that FailNow ends the test function and not the test binary
// (pluto/testing).
func CatchExitTask(f func()) bool {
	f()
	return takeGoexit()
}

// CatchAllocFree completes one mandatory allocator release and reports its
// ExitTask request. Its typed callback retains the ordinary Free effect graph.
// The compiler gives this exact internal hook the same catch boundary as
// CatchExitTask, without introducing a func() callback into that graph.
func CatchAllocFree(a Allocator, p unsafe.Pointer, size, align uintptr) bool {
	a.Free(p, size, align)
	return takeGoexit()
}

// Panicking reports whether the running task is running its deferred calls
// for a panic. sync.Once uses it to keep its lock when the function passed
// to Do panics, and to release it when that function ends the task.
func Panicking() bool {
	t := current()
	return t != nil && t.panicking != nil
}

// ResumeAllocFreeExit ends the task with the ExitTask that CatchAllocFree
// caught, once the allocator that called it has finished its own cleanup.
// The compiler takes a call of it to end with Goexit only when some
// allocator's Free may (lower/goexit.go), so that an allocator which passes
// on its parent's exits does not make every Free call a possible exit. It
// sets the flag itself: naming ExitTask here would make every program look
// like one that calls it, and check for an exit after every possible call.
func ResumeAllocFreeExit() {
	setGoexit()
}

// mainGoexited is set once main has called ExitTask.
var mainGoexited bool

// mainGoexit ends the main task after it called ExitTask and its frames have
// run their deferred calls. Without other tasks the program stops at once;
// otherwise it waits, and the scheduler stops it once nothing can run.
func mainGoexit() {
	mainGoexited = true
	if !tasking {
		noTasksLeft()
	}
	mainWait()
}

// mainWait waits for the other tasks after main ended with ExitTask. The
// compiler gives it its body once the whole program is lowered: a program
// that starts no tasks has no one to wait for and stops (mainWaitDirect),
// so that it needs no scheduler (language spec section 19.5), and any other
// parks the main task for good (mainWaitParked).
func mainWait()

func mainWaitDirect() { noTasksLeft() }

func mainWaitParked() {
	for {
		park(WaitSystem)
	}
}

// noTasksLeft stops the program after main called ExitTask and no other
// task can run.
func noTasksLeft() {
	const msg = "fatal error: no tasks (main called rt.ExitTask) - deadlock!\n"
	consoleWrite(unsafe.Pointer(unsafe.StringData(msg)), len(msg))
	exit(2)
}

// goexitFromC stops the program when ExitTask would leave a function that C
// code called (a //pu:export wrapper): C frames cannot be left this way.
func goexitFromC() {
	panicMessage("rt.ExitTask cannot leave a function called from C", "", "", "", "")
}
