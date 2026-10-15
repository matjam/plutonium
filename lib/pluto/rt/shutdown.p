package rt

// Normal termination (section 15.4) closes admission in the same critical
// section that observes every task and application timer retired.
var shutdown struct {
	mu     Lock
	work   uint64
	closed bool
	waiter *Task
}

// Installed only once task operations reach startTasks, so shutdown does
// not introduce a scheduler into a program that never needs one.
var shutdownWaitFn func()

func shutdownEnter() {
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
	shutdown.mu.Unlock()
}

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
func shutdownFailedStart(submitted *bool) {
	if !*submitted {
		shutdownLeave()
	}
}

func normalShutdown() {
	if f := shutdownWaitFn; f != nil {
		f()
	} else if !tasking {
		// Task and application-timer admission starts tasking first.
		// With neither, only this startup context can close admission.
		shutdown.closed = true
	} else {
		shutdown.mu.Lock()
		shutdown.closed = true
		shutdown.mu.Unlock()
	}
	flushShutdownCaches()
}

func waitShutdown() {
	for {
		shutdown.mu.Lock()
		if shutdown.work == 0 {
			shutdown.closed = true
			shutdown.mu.Unlock()
			return
		}
		shutdown.waiter = current()
		shutdown.mu.Unlock()
		park(WaitExit)
	}
}

func normalShutdownFinish() {
	flushShutdownCaches()
}
