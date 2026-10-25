package rt

import "unsafe"

// ----------------------------------------------------------------------------
// The CPU profiler. An interval timer counting the process's CPU time
// (ITIMER_PROF) sends SIGPROF hz times a second of CPU time used, to the
// thread that is running; the handler walks the interrupted stack and adds
// it to a ProfTable. The handler allocates nothing and takes no locks. It
// reads only the stack it interrupted, within that stack's bounds, and
// records only the interrupted address where it cannot tell the bounds (a
// scheduling or signal stack).

const (
	sigPROF      = 27
	sysSetitimer = 38
	itimerProf   = 2

	// maxProfFrames is the deepest stack a sample records; deeper stacks
	// keep their innermost frames.
	maxProfFrames = 128
)

type itimerval struct {
	intervalSec, intervalUsec int64
	valueSec, valueUsec       int64
}

type cpuProfiler struct {
	on        uint32 // the handler records (atomic)
	inflight  uint32 // handlers recording now (atomic)
	running   bool
	installed bool
	period    int64 // nanoseconds of CPU time a sample stands for
	tab       ProfTable
}

var cpuProf cpuProfiler

// StartCPUProfile starts sampling the program's stacks hz times a second of
// CPU time, into a table of tableBytes bytes of fresh memory. It reports
// false if the profiler is already running or the memory cannot be had.
// The table holds each distinct stack once, with its sample count and the
// CPU time it stands for; StopCPUProfile returns it.
func StartCPUProfile(hz int, tableBytes uintptr) bool {
	if cpuProf.running || hz <= 0 || hz > 1000000 {
		return false
	}
	mem := Mmap(tableBytes)
	if mem == nil {
		return false
	}
	IndexFuncs()
	InitProfTable(&cpuProf.tab, mem, tableBytes)
	cpuProf.period = 1000000000 / int64(hz)
	if !cpuProf.installed {
		// The handler stays installed once the profiler has run: SIGPROF's
		// default action ends the process, and a signal can still be on
		// its way after the timer stops.
		var pcs [2]uintptr
		signalPCs(&pcs)
		h := sigprof
		sa := sigactionT{
			handler:  *(*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&h))),
			flags:    saSiginfo | saOnstack | saRestorer | saRestart,
			restorer: pcs[1],
			// No preemption while the handler runs: it would switch tasks
			// from inside it.
			mask: 1 << (sigURG - 1),
		}
		syscall6(sysRtSigaction, sigPROF, uintptr(unsafe.Pointer(&sa)), 0, 8, 0, 0)
		cpuProf.installed = true
	}
	cpuProf.running = true
	store32(&cpuProf.on, 1)
	usec := int64(1000000 / hz)
	if usec == 0 {
		usec = 1
	}
	it := itimerval{intervalUsec: usec, valueUsec: usec}
	syscall3(sysSetitimer, itimerProf, uintptr(unsafe.Pointer(&it)), 0)
	return true
}

// StopCPUProfile stops the profiler and returns its table, or nil if it was
// not running. When it returns no handler is adding to the table any more.
// The table's memory is never freed: the strings and stacks read from it
// stay valid.
func StopCPUProfile() *ProfTable {
	if !cpuProf.running {
		return nil
	}
	var it itimerval
	syscall3(sysSetitimer, itimerProf, uintptr(unsafe.Pointer(&it)), 0)
	// A locked exchange orders the store before the load of inflight, as
	// the handler's locked increment orders its own (see sigprof).
	xchg32(&cpuProf.on, 0)
	for load32(&cpuProf.inflight) != 0 {
		pause()
	}
	cpuProf.running = false
	return &cpuProf.tab
}

// sigprof is the SIGPROF handler. A handler that sees on set after it has
// announced itself in inflight records into a table StopCPUProfile has not
// yet returned; one that sees it clear records nothing.
func sigprof(sig int32, info, ctx unsafe.Pointer) {
	if load32(&cpuProf.on) == 0 {
		return
	}
	xadd32(&cpuProf.inflight, 1)
	if load32(&cpuProf.on) != 0 {
		pc := *(*uintptr)(unsafe.Add(ctx, 40+16*8)) // uc_mcontext.gregs[REG_RIP]
		sp := *(*uintptr)(unsafe.Add(ctx, 40+15*8)) // gregs[REG_RSP]
		bp := *(*uintptr)(unsafe.Add(ctx, 40+10*8)) // gregs[REG_RBP]
		var stk [maxProfFrames]uintptr
		n := 0
		tab := traceTable()
		if findFunc(tab, pc) == nil {
			// Code the table does not describe: assembly, or the vDSO.
			stk[0] = pc
			n = 1
		}
		if hi := profStackTop(sp); hi != 0 {
			var fw frameWalk
			fw.lo, fw.hi = sp, hi
			fw.start(tab, pc, sp, bp, true)
			n += walkPCs(&fw, stk[n:])
		} else if n == 0 {
			stk[0] = pc
			n = 1
		}
		cpuProf.tab.Add(stk[:n], 1, cpuProf.period)
	}
	xadd32(&cpuProf.inflight, -1)
}

// profStackTop returns the top of the stack that sp is in, if it is the
// running task's or the main thread's, or 0.
func profStackTop(sp uintptr) uintptr {
	if tasking && hs.started {
		if t := getg(); t != nil && t.stackLo != 0 && sp >= t.stackLo && sp < t.stackHi {
			return t.stackHi
		}
	}
	if stackHi != 0 && sp < stackHi && (stackLo == 0 || sp >= stackLo) && stackHi-sp < stackSize+pageSize {
		return stackHi
	}
	return 0
}

// IndexFuncs makes looking up the function at an address take logarithmic
// time instead of linear, as a profiler needs: it sorts the trace table's
// entries by address if they are not already. A profiler calls it before it
// starts, from ordinary code; the index is in pages it maps, once, and never frees.
func IndexFuncs() {
	if funcIndexed {
		return
	}
	tab := traceTable()
	n := int(*(*uint32)(tab))
	sorted := true
	for i := 1; i < n && sorted; i++ {
		sorted = *(*uintptr)(unsafe.Add(tab, traceHeaderSize+(i-1)*traceFuncSize)) <=
			*(*uintptr)(unsafe.Add(tab, traceHeaderSize+i*traceFuncSize))
	}
	if !sorted {
		idx := Mmap(uintptr(n*8 + 8))
		for i := 0; i < n; i++ {
			*(*unsafe.Pointer)(unsafe.Add(idx, i*8)) = unsafe.Add(tab, traceHeaderSize+i*traceFuncSize)
		}
		heapSortFuncs(idx, n)
		funcIndex = idx
	}
	funcIndexed = true
}
