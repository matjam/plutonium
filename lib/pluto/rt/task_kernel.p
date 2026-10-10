//pu:build kernel

package rt

// In kernel mode the current-task slot is a per-CPU variable at offset 8 of
// every per-CPU block, right after pluto.percpu.self (abi.md sections 10.6
// and 12.3): one GS-relative load. The kernel points GS at the processor's
// block before its first go statement or blocking operation, which adopts
// the running code as BootTask; every other processor's slot is nil (the
// initial image's value) until Switch fills it.

//pu:percpu
var currentTask *Task

// current returns the running task: BootTask until tasks start.
func current() *Task {
	if !tasking {
		return BootTask()
	}
	return currentTask
}

func setCurrent(t *Task) { currentTask = t }
