package rt

// Normal termination (language spec section 15.4, When main returns). Once
// main has returned, one transition under shutdown.mu observes whether any
// other application task is running or any application callback is in
// flight. With none, it closes admission of new work and the program
// cleans up its packages; otherwise the program ends at once, as os.Exit
// ends it, and the tasks that remain are not waited for.
var shutdown struct {
	mu Lock

	// running counts the application tasks that have not finished their
	// function (with its deferred calls and drops) and the application
	// callbacks in flight. Tasks that are services of a library (the
	// runner that starts the functions of time.AfterFunc) are not counted.
	running uint64

	// work counts everything running counts and the tasks that have ended
	// but are not yet retired (their stack and task record released), and
	// the service tasks. Package cleanup waits for it to reach zero.
	work uint64

	// closed says package cleanup has begun: no task or callback may start.
	closed bool

	waiter *Task
}

// shutdownGraceNs is how long main waits, in nanoseconds of the scheduler's
// Now clock, for tasks that have not yet finished before it decides that they
// are running (waitShutdown). It is a wall-clock bound, so that the time a
// program takes to end does not depend on how many tasks compete for the
// processors.
const shutdownGraceNs = 1000000

// shutdownGraceYields is the bound for a scheduler without a clock (Now is
// optional for a scheduler that runs no timers): how many times main yields.
const shutdownGraceYields = 200

// schedNowOpt is the Now method of the bound scheduler, or 0 if it has none.
// Unlike Now it does not make the program a user of timers.
func schedNowOpt() int64

// abandoned is the number of tasks and callbacks that were still running
// when main returned and the program ended without waiting for them, or 0
// (TasksAtExit).
var abandoned uint64

// shutdownCancelFn is set by a library that keeps application timers (the
// time package): it cancels the timers that are still pending, as Stop does,
// when package cleanup begins.
var shutdownCancelFn func()

// Installed only once task operations reach startTasks, so shutdown does
// not introduce a scheduler into a program that never needs one.
var shutdownWaitFn func() bool

// TasksAtExit returns the number of application tasks that were still
// running when main returned, so that the program ended without package
// cleanup (language spec section 15.4), or 0. The debug allocator uses it
// to say why it skips its leak report.
func TasksAtExit() int { return int(abandoned) }

// SetShutdownCancel registers f, which cancels the pending application
// timers of a library when package cleanup begins. There is one function; a
// second registration replaces the first.
func SetShutdownCancel(f func()) { shutdownCancelFn = f }

// shutdownEnter counts a task being started. A service task is counted for
// retirement only.
func shutdownEnter(service bool) {
	shutdown.mu.Lock()
	if shutdown.closed {
		shutdown.mu.Unlock()
		panicMessage("starting application work during package cleanup", "", "", "", "")
	}
	if shutdown.work == ^uint64(0) {
		shutdown.mu.Unlock()
		panicMessage("too much application work", "", "", "", "")
	}
	shutdown.work++
	if !service {
		shutdown.running++
	}
	shutdown.mu.Unlock()
}

// shutdownAdmit panics if package cleanup has begun: arming an application
// timer then would schedule a callback.
func shutdownAdmit() {
	shutdown.mu.Lock()
	closed := shutdown.closed
	shutdown.mu.Unlock()
	if closed {
		panicMessage("starting application work during package cleanup", "", "", "", "")
	}
}

// shutdownEnded uncounts a task whose function has finished. The task is
// still counted for retirement (shutdownLeave).
func shutdownEnded(service bool) {
	if service {
		return
	}
	shutdown.mu.Lock()
	if shutdown.running == 0 {
		shutdown.mu.Unlock()
		panicMessage("rt: running task count below zero", "", "", "", "")
	}
	shutdown.running--
	shutdown.mu.Unlock()
}

// shutdownLeave uncounts a task that has been retired, or a callback that
// has ended.
func shutdownLeave() {
	shutdown.mu.Lock()
	if shutdown.work == 0 {
		shutdown.mu.Unlock()
		panicMessage("rt: application work count below zero", "", "", "", "")
	}
	shutdown.work--
	var w *Task
	if shutdown.work == 0 {
		w = shutdown.waiter
		shutdown.waiter = nil
	}
	shutdown.mu.Unlock()
	if w != nil {
		Ready(w)
	}
}

// A failed start keeps its arguments in the caller. Only Spawn transfers
// the initialized task and its retirement obligation to the backend.
func shutdownFailedStart(submitted *bool, service bool) {
	if !*submitted {
		shutdownEnded(service)
		shutdownLeave()
	}
}

// CallbackBegin counts an application callback that is in flight, such as
// an expired time.AfterFunc timer whose function has not started yet. It
// reports false if package cleanup has begun, in which case the callback
// must not run: cleanup cancels what is still pending. CallbackEnd ends it.
func CallbackBegin() bool {
	shutdown.mu.Lock()
	if shutdown.closed {
		shutdown.mu.Unlock()
		return false
	}
	shutdown.work++
	shutdown.running++
	shutdown.mu.Unlock()
	return true
}

// CallbackEnd ends a callback counted by CallbackBegin.
func CallbackEnd() {
	shutdownEnded(false)
	shutdownLeave()
}

// normalShutdown is called when main has returned, with its own cleanup
// complete. It reports whether package cleanup follows: true if no other
// application task was running and no callback in flight, in which case
// admission is closed, pending application timers are canceled and the
// tasks that have ended are retired. Otherwise the program ends at once
// with main's status and nothing is cleaned up.
func normalShutdown() bool {
	ok := true
	if f := shutdownWaitFn; f != nil {
		ok = f()
	} else if !tasking {
		// Task and application-timer admission starts tasking first.
		// With neither, only this startup context can close admission.
		shutdown.closed = true
	} else {
		shutdown.mu.Lock()
		shutdown.closed = true
		shutdown.mu.Unlock()
	}
	if ok {
		flushShutdownCaches()
	}
	return ok
}

// waitShutdown is the transition with a scheduler: under the lock, it
// either finds tasks running and ends the program (false), or closes
// admission. In that case it cancels the pending timers and waits for the
// tasks that have ended to be retired, which is bounded runtime work.
func waitShutdown() bool {
	// A task that woke main (by a channel, a WaitGroup) can still be in the
	// last instructions of its function, which have not finished it yet.
	// Give such tasks a short time to finish: main yields until the last of
	// them ends or the grace has passed, measured on the clock. A task that
	// is not finished by then is running, and the program ends without
	// waiting for it.
	start := schedNowOpt()
	for i := 0; load64(&shutdown.running) != 0; i++ {
		if start != 0 {
			if schedNowOpt()-start >= shutdownGraceNs {
				break
			}
		} else if i >= shutdownGraceYields {
			break
		}
		Yield()
	}
	shutdown.mu.Lock()
	if shutdown.running != 0 {
		abandoned = shutdown.running
		shutdown.mu.Unlock()
		return false
	}
	shutdown.closed = true
	shutdown.mu.Unlock()
	if f := shutdownCancelFn; f != nil {
		f()
	}
	cancelAppTimers()
	for {
		shutdown.mu.Lock()
		if shutdown.work == 0 {
			shutdown.mu.Unlock()
			return true
		}
		shutdown.waiter = current()
		shutdown.mu.Unlock()
		park(WaitExit)
	}
}

func normalShutdownFinish() {
	flushShutdownCaches()
}
