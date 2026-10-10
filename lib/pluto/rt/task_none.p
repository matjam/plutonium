//pu:build none && !kernel

package rt

// A freestanding program outside kernel mode has no per-CPU variables, so
// its current-task slot is a single variable: such a program runs tasks on
// one processor.

var currentTask *Task

// current returns the running task: BootTask until a backend is installed.
func current() *Task {
	if !tasking {
		return BootTask()
	}
	return currentTask
}

func setCurrent(t *Task) { currentTask = t }
