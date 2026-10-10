//pu:build linux

package rt

import "unsafe"

// ----------------------------------------------------------------------------
// The hosted scheduler (language spec section 4.7). It is one
// implementation of the Scheduler interface, started the first time a
// program starts a puroutine or blocks; a program that never does links
// none of it.
//
// It follows Go's design. A P is a processor: a run queue and the right to
// run puroutines; there are as many as CPUs (or $PLUTO_MAXPROCS). An m is a
// worker thread, made with clone(2) without libc; it runs puroutines while
// it holds a P. Each P has a fixed ring of runnable puroutines that its m
// pushes to and pops from without locks, and that idle ms steal half of; a
// locked global queue takes the overflow and puroutines readied by threads
// with no P. A puroutine that makes a blocking system call between
// EnterSyscall and ExitSyscall keeps its P only until the monitor thread
// (sysmon) sees the call last, then the P moves to another m. sysmon also
// preempts a puroutine that has run for 10ms by sending its thread SIGURG;
// the handler redirects it into asyncPreempt (preemptAsync). I/O waits go
// through one epoll poller thread. Task records live at the top of their
// stacks, which come from the allocator the program asked for.

const (
	sysSchedGetaffinity = 204
	sysNanosleep        = 35
	sysGetpid           = 39
	sysFutex            = 202
	sysTgkill           = 234
	sysClockGettime     = 228
	sysArchPrctl        = 158
	sysEpollCreate1     = 291
	sysEpollCtl         = 233
	sysEpollWait        = 232

	archSetFS = 0x1002
	sigURG    = 23
	saRestart = 0x10000000

	futexWaitPrivate = 128
	futexWakePrivate = 129

	// CLONE_VM | CLONE_FS | CLONE_FILES | CLONE_SIGHAND | CLONE_THREAD |
	// CLONE_SYSVSEM | CLONE_SETTLS
	cloneFlags = 0x100 | 0x200 | 0x400 | 0x800 | 0x10000 | 0x40000 | 0x80000

	maxProcs       = 256
	runqSize       = 256
	forcePreemptNS = 10000000 // a puroutine runs at most this long before it is preempted
	syscallRetake  = 20000    // a P stays with a blocked system call at most this long if others want it
	g0StackSize    = 256 << 10
	sigStackSize   = 32 << 10
)

// P states, in the low byte of p.status; the bits above hold the owning
// m's id, so that an m leaving a system call can tell its P was not taken.
const (
	pIdle    = 0
	pRunning = 1
	pSyscall = 2
)

// An m is a worker thread. Its FS base points at it: self and curg must
// stay the first two words (sched_linux_amd64.s).
type m struct {
	self     *m
	curg     *Task   // the current-task slot, %fs:8
	g0       Context // the scheduling loop
	p        *p
	nextp    *p // the P a waker hands over
	syscallp *p // the P held during a system call
	id       uint64
	tid      int
	spinning bool // looking for work to steal
	park     uint32
	idleLink *m
	rand     uint64
}

// A p is a processor.
type p struct {
	id          uint32
	status      uint64
	m           *m
	runqhead    uint32 // taken by the owner and by thieves (atomic)
	runqtail    uint32 // advanced by the owner only (atomic)
	runq        [runqSize]*Task
	schedtick   uint32 // incremented each time the P starts running a puroutine
	syscalltick uint32 // incremented at each system call
	link        *p
	stacks      [pStackCacheMax]unsafe.Pointer // cached stacks (allocStack)
	nstacks     int

	// sysmon's view of the P.
	smSchedtick   uint32
	smSchedwhen   int64
	smSyscalltick uint32
	smSyscallwhen int64
}

// hostedSched implements Scheduler.
type hostedSched struct{}

var hs struct {
	lock       Lock
	started    bool
	nprocs     uint32
	allp       [maxProcs]*p
	pidle      *p
	npidle     uint32
	midle      *m
	nmidle     uint32
	nmspinning uint32
	mcount     uint32 // worker threads
	nextMID    uint64
	runqhead   *Task
	runqtail   *Task
	runqsize   uint32
	nmsys      uint32 // workers in system calls
	pid        int

	pollWaiting uint32 // puroutines waiting in the poller
	sysmonWait  uint32
	sysmonNote  uint32
}

var m0 m

// hosted is the default backend of hosted builds. A program may bind its
// own with //pu:scheduler, which replaces it (language spec section 4.7).
//
//pu:scheduler
var hosted hostedSched

func getm() *m
func getg() *Task
func cloneThread(flags, stack uintptr, mp, fn unsafe.Pointer) int

// singleTask is the current-task slot of a program that binds its own
// backend in a hosted build; such a backend runs tasks on one thread.
var singleTask *Task

// current returns the running task from the thread's current-task slot,
// %fs:8 (abi.md section 12.3): BootTask until tasks start, nil on a
// worker's scheduling context.
func current() *Task {
	if !tasking {
		return BootTask()
	}
	if !hs.started {
		return singleTask
	}
	return getg()
}

func setCurrent(t *Task) {
	if !hs.started {
		singleTask = t
		return
	}
	getm().curg = t
}

// startHosted turns the program's thread into the first worker, running
// the main puroutine, and starts the monitor thread. Only the main thread
// exists when it runs.
func startHosted() {
	if hs.started {
		return
	}
	n := ncpu()
	if s := getenv("PLUTO_MAXPROCS"); s != "" {
		v := 0
		for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
			v = v*10 + int(s[i]-'0')
		}
		if v > 0 {
			n = v
		}
	}
	if n > maxProcs {
		n = maxProcs
	}
	hs.nprocs = uint32(n)
	hs.pid = int(syscall3(sysGetpid, 0, 0, 0))
	for i := 0; i < n; i++ {
		pp := (*p)(mustMmap(unsafe.Sizeof(p{})))
		pp.id = uint32(i)
		hs.allp[i] = pp
	}
	m0.self = &m0
	m0.tid = int(syscall3(sysGettid, 0, 0, 0))
	m0.rand = 0x9E3779B97F4A7C15
	hs.mcount = 1
	hs.nextMID = 1
	syscall3(sysArchPrctl, archSetFS, uintptr(unsafe.Pointer(&m0)), 0)
	acquirep(&m0, hs.allp[0])
	for i := n - 1; i >= 1; i-- {
		pidleput(hs.allp[i])
	}
	MakeContext(&m0.g0, mustMmap(g0StackSize), g0StackSize, m0Main)

	var pcs [2]uintptr
	signalPCs(&pcs)
	h := sigpreempt
	sa := sigactionT{
		handler:  *(*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&h))),
		flags:    saSiginfo | saOnstack | saRestorer | saRestart,
		restorer: pcs[1],
	}
	syscall6(sysRtSigaction, sigURG, uintptr(unsafe.Pointer(&sa)), 0, 8, 0, 0)

	hs.started = true
	newThread(sysmonMain, nil, false)
}

// Init starts the backend, before the first task operation: the program's
// thread becomes the first worker, still running the main puroutine, which
// the runtime then adopts as BootTask.
func (*hostedSched) Init() { startHosted() }

func m0Main() { schedLoop(&m0) }

// ncpu returns the number of CPUs the process may run on.
func ncpu() int {
	var mask [128]byte
	r := int(syscall3(sysSchedGetaffinity, 0, uintptr(len(mask)), uintptr(unsafe.Pointer(&mask[0]))))
	if r <= 0 {
		return 1
	}
	n := 0
	for i := 0; i < r && i < len(mask); i++ {
		for b := mask[i]; b != 0; b &= b - 1 {
			n++
		}
	}
	if n < 1 {
		n = 1
	}
	return n
}

func mustMmap(n uintptr) unsafe.Pointer {
	p := Mmap(n)
	if p == nil {
		panicMessage("rt: out of memory starting the scheduler", "", "", "", "")
	}
	return p
}

// newThread starts a thread running fn with a new m, which holds pp (or no
// P) when fn starts.
func newThread(fn func(mp *m), pp *p, spinning bool) {
	// String counts must be atomic before a second thread can share a
	// string (str.p).
	SetThreaded()
	mp := (*m)(mustMmap(unsafe.Sizeof(m{})))
	mp.self = mp
	mp.nextp = pp
	mp.spinning = spinning
	mp.id = xadd64(&hs.nextMID, 1)
	mp.rand = mp.id*0x9E3779B97F4A7C15 | 1
	stack := mustMmap(g0StackSize)
	rec := *(*unsafe.Pointer)(unsafe.Pointer(&fn))
	r := cloneThread(cloneFlags, uintptr(stack)+g0StackSize, unsafe.Pointer(mp), rec)
	if r < 0 {
		panicMessage("rt: cannot create a worker thread", "", "", "", "")
	}
}

// installSigstack gives the calling thread an alternate signal stack, so
// that the fault handler can report a stack overflow and the preemption
// handler runs off the puroutine's stack.
func installSigstack() {
	st := stackT{sp: mustMmap(sigStackSize), size: sigStackSize}
	syscall3(sysSigaltstack, uintptr(unsafe.Pointer(&st)), 0, 0)
}

// mstart is where a worker thread begins, on its scheduling stack.
func mstart(mp *m) {
	mp.tid = int(syscall3(sysGettid, 0, 0, 0))
	mp.g0.onCPU = 1 // the thread starts on its scheduling context
	installSigstack()
	acquirep(mp, mp.nextp)
	mp.nextp = nil
	schedLoop(mp)
}

// schedLoop runs puroutines on the worker mp, forever.
func schedLoop(mp *m) {
	for {
		t := findRunnable(mp)
		mp.p.schedtick++
		Switch(&mp.g0, &t.ctx)
	}
}

// A hostedTask is the hosted backend's task record, at the top of the
// task's stack allocation: from the bottom, a guard page, the stack, then
// this.
type hostedTask struct {
	task  Task // at offset 0
	mem   unsafe.Pointer
	total uintptr
	alloc Allocator
}

func (*hostedSched) NewTask(req StackRequest) *Task {
	size := (req.Size + pageSize - 1) &^ (pageSize - 1)
	rec := (unsafe.Sizeof(hostedTask{}) + 15) &^ 15
	total := (pageSize + size + rec + pageSize - 1) &^ (pageSize - 1)
	mem := allocStack(total, req.Alloc)
	ht := (*hostedTask)(unsafe.Add(mem, total-rec))
	memclr(unsafe.Pointer(ht), int(rec))
	ht.mem = mem
	ht.total = total
	ht.alloc = req.Alloc
	ht.task.SetStack(uintptr(mem)+pageSize, uintptr(unsafe.Pointer(ht)))
	return &ht.task
}

func (*hostedSched) FreeTask(t *Task) {
	ht := (*hostedTask)(unsafe.Pointer(t))
	freeStack(ht.mem, ht.total, ht.alloc)
}

func (*hostedSched) Spawn(t *Task) { hostedReady(t) }

func (*hostedSched) Wake(t *Task) { hostedReady(t) }

func (*hostedSched) Park(t *Task) { Switch(&t.ctx, &getm().g0) }

func (*hostedSched) Yield(t *Task) { Switch(&t.ctx, &getm().g0) }

// hostedReady queues t on the current worker's P, or on the global queue
// from a thread without one, and wakes an idle worker to steal it.
func hostedReady(t *Task) {
	mp := getm()
	cur := mp.curg
	if cur != nil {
		inc32(&cur.locks)
	}
	if pp := mp.p; pp != nil && mp.syscallp == nil {
		runqput(pp, t)
	} else {
		hs.lock.Lock()
		globrunqput(t)
		hs.lock.Unlock()
	}
	wakep()
	if cur != nil {
		// Wake never switches (scheduler contract, rule 4): a preemption
		// that is pending waits for the caller's next EnablePreemption.
		dec32(&cur.locks)
	}
}

// findRunnable returns a task for mp to run, from its P's queue, the
// global queue or another P's queue, and puts the worker to sleep while
// there is none. It returns with mp holding a P.
func findRunnable(mp *m) *Task {
	for {
		pp := mp.p
		if pp == nil {
			stopm(mp)
			continue
		}
		if g := runqget(pp); g != nil {
			resetSpinning(mp)
			return g
		}
		if load32(&hs.runqsize) != 0 {
			hs.lock.Lock()
			g := globrunqget(pp)
			hs.lock.Unlock()
			if g != nil {
				resetSpinning(mp)
				return g
			}
		}
		n := hs.nprocs
		if n > 1 && (mp.spinning || 2*load32(&hs.nmspinning) < n-load32(&hs.npidle)) {
			if !mp.spinning {
				mp.spinning = true
				xadd32(&hs.nmspinning, 1)
			}
			// Look for a while (some tens of microseconds) before going
			// to sleep: work that arrives soon is taken without a futex
			// wakeup, which costs more than the spinning.
			for i := 0; i < 64; i++ {
				if i >= 4 {
					for k := 0; k < 64; k++ {
						pause()
					}
					if load32(&hs.runqsize) != 0 {
						break
					}
				}
				start := uint32(mrand(mp)) % n
				for k := uint32(0); k < n; k++ {
					p2 := hs.allp[(start+k)%n]
					if p2 == pp {
						continue
					}
					if g := runqsteal(pp, p2); g != nil {
						resetSpinning(mp)
						return g
					}
				}
			}
		}

		// Nothing to run: give up the P.
		hs.lock.Lock()
		if hs.runqsize != 0 {
			g := globrunqget(pp)
			hs.lock.Unlock()
			resetSpinning(mp)
			return g
		}
		releasep(mp)
		pidleput(pp)
		hs.lock.Unlock()
		if mp.spinning {
			mp.spinning = false
			xadd32(&hs.nmspinning, -1)
		}
		// Work queued before the P went idle may have found this worker
		// still spinning and woken nobody: look again.
		if workAvailable() {
			hs.lock.Lock()
			p2 := pidleget()
			hs.lock.Unlock()
			if p2 != nil {
				acquirep(mp, p2)
				continue
			}
		}
		stopm(mp)
	}
}

// workAvailable reports whether any queue holds a puroutine.
func workAvailable() bool {
	if load32(&hs.runqsize) != 0 {
		return true
	}
	for i := uint32(0); i < hs.nprocs; i++ {
		if !runqempty(hs.allp[i]) {
			return true
		}
	}
	return false
}

// resetSpinning ends mp's search for work. If it was the last worker
// searching, another is woken in case there is more.
func resetSpinning(mp *m) {
	if mp.spinning {
		mp.spinning = false
		if xadd32(&hs.nmspinning, -1) == 0 {
			wakep()
		}
	}
}

// wakep wakes an idle worker to look for work, unless one is already
// looking or no P is free.
func wakep() {
	if load32(&hs.npidle) == 0 {
		return
	}
	if load32(&hs.nmspinning) != 0 || !cas32(&hs.nmspinning, 0, 1) {
		return
	}
	startm(nil, true)
}

// startm gives pp (or an idle P) to an idle worker, or a new one, and
// wakes it.
func startm(pp *p, spinning bool) {
	hs.lock.Lock()
	if pp == nil {
		pp = pidleget()
		if pp == nil {
			hs.lock.Unlock()
			if spinning {
				xadd32(&hs.nmspinning, -1)
			}
			return
		}
	}
	mp := hs.midle
	if mp != nil {
		hs.midle = mp.idleLink
		hs.nmidle--
	} else {
		hs.mcount++
	}
	hs.lock.Unlock()
	if mp == nil {
		newThread(mstart, pp, spinning)
		return
	}
	mp.spinning = spinning
	mp.nextp = pp
	notewakeup(&mp.park)
}

// stopm puts mp to sleep until startm gives it a P.
func stopm(mp *m) {
	hs.lock.Lock()
	mp.idleLink = hs.midle
	hs.midle = mp
	hs.nmidle++
	checkDead()
	hs.lock.Unlock()
	notesleep(&mp.park)
	noteclear(&mp.park)
	acquirep(mp, mp.nextp)
	mp.nextp = nil
}

// checkDead stops the program when no puroutine can ever run again: every
// worker is asleep (a worker in a system call is not: the call may
// return), nothing waits in the poller and no queue holds a puroutine.
// Called with hs.lock held.
func checkDead() {
	if hs.mcount-hs.nmidle > 0 || load32(&hs.pollWaiting) != 0 || workAvailable() {
		return
	}
	printString("fatal error: all puroutines are asleep - deadlock!\n")
	exit(2)
}

func acquirep(mp *m, pp *p) {
	mp.p = pp
	pp.m = mp
	store64(&pp.status, pRunning|mp.id<<8)
	if load32(&hs.sysmonWait) != 0 && cas32(&hs.sysmonWait, 1, 0) {
		notewakeup(&hs.sysmonNote)
	}
}

func releasep(mp *m) {
	pp := mp.p
	pp.m = nil
	mp.p = nil
}

// pidleput and pidleget keep the list of idle Ps; hs.lock is held.
func pidleput(pp *p) {
	pp.m = nil
	store64(&pp.status, pIdle)
	pp.link = hs.pidle
	hs.pidle = pp
	xadd32(&hs.npidle, 1)
}

func pidleget() *p {
	pp := hs.pidle
	if pp != nil {
		hs.pidle = pp.link
		xadd32(&hs.npidle, -1)
	}
	return pp
}

// ----------------------------------------------------------------------------
// Run queues

// runqput puts g at the tail of pp's queue, moving half of a full queue to
// the global one. Only pp's owner calls it.
func runqput(pp *p, g *Task) {
	for {
		h := load32(&pp.runqhead)
		t := pp.runqtail
		if t-h < runqSize {
			pp.runq[t%runqSize] = g
			store32(&pp.runqtail, t+1)
			return
		}
		if runqputslow(pp, g, h, t) {
			return
		}
	}
}

func runqputslow(pp *p, g *Task, h, t uint32) bool {
	n := (t - h) / 2
	var batch [runqSize/2 + 1]*Task
	for i := uint32(0); i < n; i++ {
		batch[i] = pp.runq[(h+i)%runqSize]
	}
	if !cas32(&pp.runqhead, h, h+n) {
		return false
	}
	batch[n] = g
	hs.lock.Lock()
	for i := uint32(0); i <= n; i++ {
		globrunqput(batch[i])
	}
	hs.lock.Unlock()
	return true
}

// runqget takes the head of pp's queue. Only pp's owner calls it.
func runqget(pp *p) *Task {
	for {
		h := load32(&pp.runqhead)
		t := pp.runqtail
		if t == h {
			return nil
		}
		g := pp.runq[h%runqSize]
		if cas32(&pp.runqhead, h, h+1) {
			return g
		}
	}
}

func runqempty(pp *p) bool { return load32(&pp.runqhead) == load32(&pp.runqtail) }

// runqsteal moves half of p2's queue to pp's, which is empty, and returns
// one of the puroutines taken.
func runqsteal(pp, p2 *p) *Task {
	t := pp.runqtail
	var n uint32
	for {
		h := load32(&p2.runqhead)
		t2 := load32(&p2.runqtail)
		n = t2 - h
		n = n - n/2
		if n == 0 {
			return nil
		}
		if n > runqSize/2 {
			continue // inconsistent snapshot
		}
		for i := uint32(0); i < n; i++ {
			pp.runq[(t+i)%runqSize] = p2.runq[(h+i)%runqSize]
		}
		if cas32(&p2.runqhead, h, h+n) {
			break
		}
	}
	n--
	g := pp.runq[(t+n)%runqSize]
	if n > 0 {
		store32(&pp.runqtail, t+n)
	}
	return g
}

// globrunqput and globrunqget use the global queue; hs.lock is held.
func globrunqput(g *Task) {
	g.Link = nil
	if hs.runqtail != nil {
		hs.runqtail.Link = g
	} else {
		hs.runqhead = g
	}
	hs.runqtail = g
	xadd32(&hs.runqsize, 1)
}

func globrunqget(pp *p) *Task {
	n := hs.runqsize
	if n == 0 {
		return nil
	}
	if k := n/hs.nprocs + 1; n > k {
		n = k
	}
	if n > runqSize/2 {
		n = runqSize / 2
	}
	g := globrunqpop()
	for i := uint32(1); i < n; i++ {
		runqput(pp, globrunqpop())
	}
	return g
}

func globrunqpop() *Task {
	g := hs.runqhead
	hs.runqhead = g.Link
	if hs.runqhead == nil {
		hs.runqtail = nil
	}
	g.Link = nil
	xadd32(&hs.runqsize, -1)
	return g
}

func mrand(mp *m) uint64 {
	x := mp.rand
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	mp.rand = x
	return x * 0x2545F4914F6CDD1D
}

// ----------------------------------------------------------------------------
// System calls

// EnterSyscall tells the scheduler that the running puroutine is about to
// make a system call that may block. Until ExitSyscall its worker's P may
// move to another worker, so that the other puroutines keep running. The
// puroutine is not preempted in between. It costs a few atomic operations,
// and nothing before the scheduler has started.
func EnterSyscall() {
	if !hs.started {
		return
	}
	mp := getm()
	g := mp.curg
	if g == nil || g.insyscall || g.locks != 0 {
		// With preemption disabled (a panic, a runtime critical section)
		// the P stays put, so that ExitSyscall never switches away.
		return
	}
	inc32(&g.locks)
	g.insyscall = true
	pp := mp.p
	pp.syscalltick++
	mp.syscallp = pp
	xadd32(&hs.nmsys, 1)
	store64(&pp.status, pSyscall|mp.id<<8)
}

// ExitSyscall ends the system call EnterSyscall announced. If the P was
// handed on meanwhile, the puroutine takes an idle one or waits for one.
func ExitSyscall() {
	if !hs.started {
		return
	}
	mp := getm()
	g := mp.curg
	if g == nil || !g.insyscall {
		return
	}
	g.insyscall = false
	pp := mp.syscallp
	mp.syscallp = nil
	if cas64(&pp.status, pSyscall|mp.id<<8, pRunning|mp.id<<8) {
		xadd32(&hs.nmsys, -1)
		enablePreempt(g)
		return
	}
	mp.p = nil
	hs.lock.Lock()
	p2 := pidleget()
	hs.lock.Unlock()
	if p2 != nil {
		acquirep(mp, p2)
		xadd32(&hs.nmsys, -1)
		enablePreempt(g)
		return
	}
	// No P is free: queue the task (afterSwitch wakes it onto the global
	// queue) and let this worker sleep.
	xadd32(&hs.nmsys, -1)
	g.status = tYielding
	Switch(&g.ctx, &mp.g0)
	g.status = tRunning
	enablePreempt(g)
}

// ----------------------------------------------------------------------------
// The monitor thread

func sysmonMain(mp *m) {
	mp.tid = int(syscall3(sysGettid, 0, 0, 0))
	delay := int64(20000)
	idle := 0
	for {
		if idle == 0 {
			delay = 20000
		} else if idle > 50 {
			delay *= 2
		}
		if delay > 10000000 {
			delay = 10000000
		}
		nanosleep(delay)
		if load32(&hs.npidle) == hs.nprocs && load32(&hs.nmsys) == 0 {
			// Nothing runs: sleep until a worker takes a P.
			store32(&hs.sysmonNote, 0)
			store32(&hs.sysmonWait, 1)
			if load32(&hs.npidle) == hs.nprocs && load32(&hs.nmsys) == 0 {
				notesleep(&hs.sysmonNote)
			}
			store32(&hs.sysmonWait, 0)
			idle = 0
			continue
		}
		if retake(nanotime()) {
			idle = 0
		} else {
			idle++
		}
	}
}

// retake preempts puroutines that have run too long and takes Ps away from
// workers blocked in system calls. It reports whether it did anything.
func retake(now int64) bool {
	took := false
	for i := uint32(0); i < hs.nprocs; i++ {
		pp := hs.allp[i]
		st := load64(&pp.status)
		switch st & 0xff {
		case pRunning:
			t := load32(&pp.schedtick)
			if pp.smSchedtick != t {
				pp.smSchedtick = t
				pp.smSchedwhen = now
			} else if now-pp.smSchedwhen >= forcePreemptNS {
				pp.smSchedwhen = now
				if mp := pp.m; mp != nil && mp.curg != nil {
					syscall3(sysTgkill, uintptr(hs.pid), uintptr(mp.tid), sigURG)
					took = true
				}
			}
		case pSyscall:
			t := load32(&pp.syscalltick)
			if pp.smSyscalltick != t {
				pp.smSyscalltick = t
				pp.smSyscallwhen = now
				continue
			}
			if runqempty(pp) && load32(&hs.runqsize) == 0 && now-pp.smSyscallwhen < forcePreemptNS {
				continue
			}
			if now-pp.smSyscallwhen < syscallRetake {
				continue
			}
			if cas64(&pp.status, st, pIdle) {
				took = true
				handoffp(pp)
			}
		}
	}
	return took
}

// handoffp finds a worker for pp, taken from a blocked system call.
func handoffp(pp *p) {
	if !runqempty(pp) || load32(&hs.runqsize) != 0 {
		startm(pp, false)
		return
	}
	if load32(&hs.nmspinning)+load32(&hs.npidle) == 0 && cas32(&hs.nmspinning, 0, 1) {
		startm(pp, true)
		return
	}
	hs.lock.Lock()
	pidleput(pp)
	hs.lock.Unlock()
}

// sigpreempt handles SIGURG, sent by sysmon to a worker whose puroutine has
// run for a full time slice.
func sigpreempt(sig int32, info, ctx unsafe.Pointer) {
	preemptAsync((*uintptr)(unsafe.Add(ctx, 40+16*8))) // uc_mcontext.gregs[REG_RIP]
}

// ----------------------------------------------------------------------------
// The poller

const (
	epollIn      = 0x1
	epollOut     = 0x4
	epollRdhup   = 0x2000
	epollOneshot = 0x40000000
	epollCtlAdd  = 1
	epollCtlMod  = 3
	epollCloexec = 0x80000
	errNoent     = 2
)

var poller struct {
	lock    Lock
	started bool
	fd      int
}

type pollWaiter struct {
	g *Task
}

// PollWait parks the running puroutine until the file descriptor fd, which
// must be in non-blocking mode, is ready for reading (or for writing, if
// write is set), and reports whether it could wait. One puroutine at a
// time may wait on a descriptor. A descriptor closed while a puroutine
// waits on it does not wake it.
//
//pu:blocking
func PollWait(fd int, write bool) bool {
	startTasks()
	if !hs.started {
		return false // a program's own backend: no poller
	}
	g := current()
	if g == nil {
		return false
	}
	startPoller()
	var w pollWaiter
	w.g = g
	var ev [12]byte // struct epoll_event, packed: events, then data
	events := uint32(epollIn | epollRdhup | epollOneshot)
	if write {
		events = epollOut | epollOneshot
	}
	*(*uint32)(unsafe.Pointer(&ev[0])) = events
	*(*uintptr)(unsafe.Pointer(&ev[4])) = uintptr(unsafe.Pointer(&w))
	xadd32(&hs.pollWaiting, 1)
	r := syscall6(sysEpollCtl, uintptr(poller.fd), epollCtlMod, uintptr(fd), uintptr(unsafe.Pointer(&ev[0])), 0, 0)
	if r == ^uintptr(errNoent-1) {
		r = syscall6(sysEpollCtl, uintptr(poller.fd), epollCtlAdd, uintptr(fd), uintptr(unsafe.Pointer(&ev[0])), 0, 0)
	}
	if r != 0 {
		xadd32(&hs.pollWaiting, -1)
		return false
	}
	Park()
	return true
}

func startPoller() {
	poller.lock.Lock()
	if !poller.started {
		poller.fd = int(syscall3(sysEpollCreate1, epollCloexec, 0, 0))
		poller.started = true
		newThread(pollerMain, nil, false)
	}
	poller.lock.Unlock()
}

func pollerMain(mp *m) {
	mp.tid = int(syscall3(sysGettid, 0, 0, 0))
	var evs [64 * 12]byte
	for {
		n := int(syscall6(sysEpollWait, uintptr(poller.fd), uintptr(unsafe.Pointer(&evs[0])), 64, ^uintptr(0), 0, 0))
		for i := 0; i < n; i++ {
			w := (*pollWaiter)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(&evs[i*12+4]))))
			Ready(w.g)
			xadd32(&hs.pollWaiting, -1)
		}
	}
}

// ----------------------------------------------------------------------------
// Thread primitives

// lockAcquire, lockTry and lockRelease implement Lock with a futex: 0 is
// free, 1 held, 2 held with sleepers.
func lockAcquire(l *Lock) {
	if cas32(&l.key, 0, 1) {
		return
	}
	for i := 0; i < 64; i++ {
		pause()
		if load32(&l.key) == 0 && cas32(&l.key, 0, 1) {
			return
		}
	}
	for xchg32(&l.key, 2) != 0 {
		futexsleep(&l.key, 2)
	}
}

func lockTry(l *Lock) bool { return cas32(&l.key, 0, 1) }

func lockRelease(l *Lock) {
	if xchg32(&l.key, 0) == 2 {
		futexwakeup(&l.key, 1)
	}
}

func futexsleep(addr *uint32, val uint32) {
	syscall6(sysFutex, uintptr(unsafe.Pointer(addr)), futexWaitPrivate, uintptr(val), 0, 0, 0)
}

func futexwakeup(addr *uint32, n int) {
	syscall6(sysFutex, uintptr(unsafe.Pointer(addr)), futexWakePrivate, uintptr(n), 0, 0, 0)
}

// A note is a one-shot event for one sleeping thread.
func notesleep(n *uint32) {
	for load32(n) == 0 {
		futexsleep(n, 0)
	}
}

func notewakeup(n *uint32) {
	store32(n, 1)
	futexwakeup(n, 1)
}

func noteclear(n *uint32) { store32(n, 0) }

type timespec struct {
	sec  int64
	nsec int64
}

func nanotime() int64 {
	var ts timespec
	syscall3(sysClockGettime, 1, uintptr(unsafe.Pointer(&ts)), 0) // CLOCK_MONOTONIC
	return ts.sec*1000000000 + ts.nsec
}

func nanosleep(ns int64) {
	ts := timespec{sec: ns / 1000000000, nsec: ns % 1000000000}
	syscall3(sysNanosleep, uintptr(unsafe.Pointer(&ts)), 0, 0)
}

// getenv returns the value of the environment variable key, or "".
func getenv(key string) string {
	env := envp
	if env == nil {
		return ""
	}
	for i := 0; ; i++ {
		s := *(**byte)(unsafe.Add(env, i*8))
		if s == nil {
			return ""
		}
		n := 0
		for *(*byte)(unsafe.Add(unsafe.Pointer(s), n)) != 0 {
			n++
		}
		kv := unsafe.String(s, n)
		if len(kv) > len(key) && kv[len(key)] == '=' && strequal(kv[:len(key)], key) {
			return kv[len(key)+1:]
		}
	}
}

// protectGuard makes the guard page at the bottom of a stack inaccessible,
// or accessible again before the stack is freed.
func protectGuard(mem unsafe.Pointer, access bool) {
	prot := ProtNone
	if access {
		prot = ProtRead | ProtWrite
	}
	Mprotect(mem, pageSize, prot)
}

// Stacks of one size from the program-wide default allocator are kept in
// caches, guard page and all, since mapping and protecting pages costs
// several system calls: a few per P, used without locks, in front of a
// shared one. Builds that track allocation sites (the debug allocator)
// cache nothing, so every stack is checked as it is freed.

const (
	stackCacheMax  = 64
	pStackCacheMax = 8
)

var stackCache struct {
	lock  Lock
	n     int
	total uintptr // the size of the cached stacks, fixed by the first one
	a     Allocator
	mem   [stackCacheMax]unsafe.Pointer
}

// cacheable reports whether a stack of total bytes from a may be cached.
func cacheable(total uintptr, a Allocator) bool {
	if TrackSites || a != defaultAllocator {
		return false
	}
	c := &stackCache
	if c.total == 0 {
		c.lock.Lock()
		if c.total == 0 {
			c.total, c.a = total, a
		}
		c.lock.Unlock()
	}
	return c.total == total && c.a == a
}

// pinP disables preemption and returns the running worker's P, or nil.
func pinP() *p {
	mp := getm()
	if t := mp.curg; t != nil {
		inc32(&t.locks)
	}
	if mp.syscallp != nil {
		return nil
	}
	return mp.p
}

func unpinP() {
	if t := getm().curg; t != nil {
		dec32(&t.locks)
	}
}

func allocStack(total uintptr, a Allocator) unsafe.Pointer {
	if hs.started && cacheable(total, a) {
		pp := pinP()
		if pp != nil && pp.nstacks > 0 {
			pp.nstacks--
			mem := pp.stacks[pp.nstacks]
			unpinP()
			return mem
		}
		unpinP()
		c := &stackCache
		if c.n > 0 {
			c.lock.Lock()
			if c.n > 0 {
				c.n--
				mem := c.mem[c.n]
				c.lock.Unlock()
				return mem
			}
			c.lock.Unlock()
		}
	}
	mem := allocAt(a, total, pageSize, StackSite)
	protectGuard(mem, false)
	return mem
}

func freeStack(mem unsafe.Pointer, total uintptr, a Allocator) {
	if hs.started && cacheable(total, a) {
		pp := pinP()
		if pp != nil && pp.nstacks < pStackCacheMax {
			pp.stacks[pp.nstacks] = mem
			pp.nstacks++
			unpinP()
			return
		}
		unpinP()
		c := &stackCache
		c.lock.Lock()
		if c.n < stackCacheMax {
			c.mem[c.n] = mem
			c.n++
			c.lock.Unlock()
			return
		}
		c.lock.Unlock()
	}
	protectGuard(mem, true)
	a.Free(mem, total, pageSize)
}

// flushStackCache frees the cached stacks at exit, so that an allocator
// that reports leaks sees them returned.
func flushStackCache() {
	c := &stackCache
	if c.total == 0 {
		return
	}
	c.lock.Lock()
	for c.n > 0 {
		c.n--
		protectGuard(c.mem[c.n], true)
		c.a.Free(c.mem[c.n], c.total, pageSize)
	}
	// The Ps' own caches are left alone: their workers may be using them
	// while the program exits.
	c.lock.Unlock()
}

// ----------------------------------------------------------------------------
// Pinning to a P, for per-processor caches

// ProcPin disables preemption and returns the index of the P the calling
// thread holds, below MaxProcs, or -1 if it holds none. Until the scheduler
// starts it returns 0. ProcUnpin undoes it. Per-P caches (the default
// allocator's) are safe to use in between without locks.
func ProcPin() int {
	if !hs.started {
		if tasking {
			// A program's own backend: no per-P caches.
			DisablePreemption()
			return -1
		}
		return 0
	}
	mp := getm()
	if t := mp.curg; t != nil {
		inc32(&t.locks)
	}
	if pp := mp.p; pp != nil && mp.syscallp == nil {
		return int(pp.id)
	}
	return -1
}

// ProcUnpin ends a ProcPin.
func ProcUnpin() {
	if !hs.started {
		if tasking {
			EnablePreemption()
		}
		return
	}
	if t := getm().curg; t != nil {
		enablePreempt(t)
	}
}

// MaxProcs is the most Ps the hosted scheduler runs.
const MaxProcs = maxProcs
