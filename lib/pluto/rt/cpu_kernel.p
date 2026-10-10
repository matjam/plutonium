//pu:build kernel

package rt

// cpu is the number of the processor whose per-CPU block holds it.
//
//pu:percpu
var cpu int

// cpuKnown is set by the first SetCPU: from then on, GS points at a per-CPU
// block wherever the kernel runs.
var cpuKnown bool

// SetCPU records n as the number of the processor it runs on, in that
// processor's per-CPU block; panics report it (Panic.CPU), and tell the
// processors apart by it. Call it on each processor once its GS base
// points at its block (abi.md section 10.6). Until the first call,
// panics report CPU 0 and do not read GS.
func SetCPU(n int) {
	cpu = n
	cpuKnown = true
}

// cpuNumber returns the number SetCPU gave this processor, or 0.
func cpuNumber() int {
	if !cpuKnown {
		return 0
	}
	return cpu
}
