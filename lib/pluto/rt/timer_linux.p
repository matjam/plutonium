//pu:build linux

package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Timers of the hosted backend (abi.md section 12.9). Each P has a heap
// (p.timers) that the puroutines running on it arm their timers in. A
// worker runs its own P's expired timers each time it looks for a
// puroutine to run, and a spinning worker runs those of the other Ps; a
// timer's puroutine is then readied onto the running worker's P. While
// workers sleep, the poller thread waits in epoll for the earliest
// deadline of every P and runs the expired timers itself (their
// puroutines go to the global queue, and an idle worker is woken). Timers
// never move between heaps, since each heap has its own lock.

const (
	sysRead        = 0
	sysEventfd2    = 290
	sysEpollPwait2 = 441

	efdNonblock    = 0x800
	efdCloexec     = 0x80000
	clockMonotonic = 1
	errNosys       = 38
	atSysinfoEhdr  = 33
)

// Now reads CLOCK_MONOTONIC: the Now clock of the hosted backend.
func (*hostedSched) Now() int64 { return nanotime() }

// ArmTimer does nothing: the hosted backend runs the timers in its
// processors' heaps itself, and never uses the shared heap that ArmTimer
// serves.
func (*hostedSched) ArmTimer(deadline int64) {}

// timerStart starts what running timers needs, before a timer is armed:
// the scheduler, and with the hosted backend the poller thread.
func timerStart() {
	startTasks()
	if hs.started {
		startPoller()
	}
}

// timerHeapLocal returns the heap for a timer armed now: the running P's,
// or the shared heap with a program's own backend.
func timerHeapLocal() *timerHeap {
	if !hs.started {
		return &globalTimers
	}
	pp := pinP()
	if pp == nil {
		pp = hs.allp[0] // a thread without a P: any heap will do
	}
	unpinP()
	return &pp.timers
}

// timerEarlier is called, with h.lock held, when the earliest deadline in
// h has become when. The poller is woken if it sleeps past when, since the
// P that holds h may go idle before then.
func timerEarlier(h *timerHeap, when int64) {
	if h == &globalTimers {
		armGlobal(when)
		return
	}
	for {
		u := load64(&poller.until)
		if when >= int64(u) {
			return
		}
		if cas64(&poller.until, u, uint64(when)) {
			one := uint64(1)
			syscall3(sysWrite, uintptr(poller.wakefd), uintptr(unsafe.Pointer(&one)), 8)
			return
		}
	}
}

// runTimersOf runs the expired timers of pp's heap and reports whether it
// ran any. now is the time if the caller has read it, or 0.
func runTimersOf(pp *p, now int64) bool {
	h := &pp.timers
	next := h.nextDeadline()
	if next == NoDeadline {
		return false
	}
	if now == 0 {
		now = nanotime()
	}
	if now < next {
		return false
	}
	h.lock.Lock()
	h.run(now)
	h.lock.Unlock()
	return true
}

// runOtherTimers runs the expired timers of every P but pp and reports
// whether it ran any.
func runOtherTimers(pp *p) bool {
	ran := false
	now := int64(0)
	for i := uint32(0); i < hs.nprocs; i++ {
		p2 := hs.allp[i]
		if p2 == pp || p2.timers.nextDeadline() == NoDeadline {
			continue
		}
		if now == 0 {
			now = nanotime()
		}
		if runTimersOf(p2, now) {
			ran = true
		}
	}
	return ran
}

// pollTimers runs, on the poller thread, the expired timers of every P,
// and returns how long the poller may sleep: until the earliest deadline,
// in nanoseconds, or -1 when no timer is armed.
//
// poller.until is NoDeadline while it looks: a timer armed meanwhile
// lowers it and writes the eventfd (timerEarlier), which wakes the epoll
// wait at once. With sequentially consistent stores, either the scan below
// sees the new timer or the arming sees NoDeadline.
func pollTimers() int64 {
	store64(&poller.until, uint64(NoDeadline))
	now := nanotime()
	next := NoDeadline
	fired := false
	for i := uint32(0); i < hs.nprocs; i++ {
		pp := hs.allp[i]
		if runTimersOf(pp, now) {
			fired = true
		}
		if w := pp.timers.nextDeadline(); w < next {
			next = w
		}
	}
	if fired {
		// What the timers did may leave every puroutine asleep for good,
		// with no worker left to notice.
		hs.lock.Lock()
		checkDead()
		hs.lock.Unlock()
	}
	if next == NoDeadline {
		return -1
	}
	if !cas64(&poller.until, uint64(NoDeadline), uint64(next)) {
		return 0 // a timer was armed meanwhile: look again
	}
	if next <= now {
		return 0
	}
	return next - now
}

var noPwait2 bool // epoll_pwait2 is missing (Linux before 5.11)

// pollerWait waits for events on the poller's epoll set for at most
// timeout nanoseconds (forever if negative) and returns how many it got.
func pollerWait(evs *[64 * 12]byte, timeout int64) int {
	ev := uintptr(unsafe.Pointer(&evs[0]))
	var r uintptr
	switch {
	case timeout < 0:
		r = syscall6(sysEpollWait, uintptr(poller.fd), ev, 64, ^uintptr(0), 0, 0)
	case !noPwait2:
		ts := timespec{sec: timeout / 1000000000, nsec: timeout % 1000000000}
		r = syscall6(sysEpollPwait2, uintptr(poller.fd), ev, 64, uintptr(unsafe.Pointer(&ts)), 0, 8)
		if r == ^uintptr(errNosys-1) {
			noPwait2 = true
			return 0
		}
	default:
		ms := (timeout + 999999) / 1000000 // round up, not to wake early
		if ms > 1<<30 {
			ms = 1 << 30
		}
		r = syscall6(sysEpollWait, uintptr(poller.fd), ev, 64, uintptr(ms), 0, 0)
	}
	if int(r) < 0 {
		return 0 // EINTR: the caller looks at the timers again
	}
	return int(r)
}

// ----------------------------------------------------------------------------
// Reading the clock through the vDSO

var vdso struct {
	done         uint32
	clockGettime uintptr // __vdso_clock_gettime, or 0
}

// clockGettime reads clock into ts, with the vDSO's clock_gettime if the
// kernel maps one, otherwise with the system call.
func clockGettime(clock uintptr, ts *timespec) {
	if load32(&vdso.done) == 0 {
		vdso.clockGettime = vdsoLookup("__vdso_clock_gettime")
		store32(&vdso.done, 1)
	}
	if fn := vdso.clockGettime; fn != 0 && vdsoCall2(fn, clock, uintptr(unsafe.Pointer(ts))) == 0 {
		return
	}
	syscall3(sysClockGettime, clock, uintptr(unsafe.Pointer(ts)), 0)
}

// vdsoCall2 calls the C function fn(a1, a2) on a 16-byte aligned stack
// (timer_linux_amd64.s).
func vdsoCall2(fn, a1, a2 uintptr) uintptr

// auxval returns the value of the auxiliary vector entry key, which
// follows the environment on the initial stack, or 0.
func auxval(key uintptr) uintptr {
	if envp == nil {
		return 0
	}
	p := uintptr(envp)
	for *(*uintptr)(unsafe.Pointer(p)) != 0 {
		p += 8
	}
	for p += 8; ; p += 16 {
		k := *(*uintptr)(unsafe.Pointer(p))
		if k == 0 {
			return 0
		}
		if k == key {
			return *(*uintptr)(unsafe.Pointer(p + 8))
		}
	}
}

func peek64(a uintptr) uint64 { return *(*uint64)(unsafe.Pointer(a)) }
func peek32(a uintptr) uint32 { return *(*uint32)(unsafe.Pointer(a)) }
func peek16(a uintptr) uint16 { return *(*uint16)(unsafe.Pointer(a)) }
func peek8(a uintptr) uint8   { return *(*uint8)(unsafe.Pointer(a)) }

// vdsoLookup returns the address of the global function name in the
// vDSO's dynamic symbol table, or 0.
func vdsoLookup(name string) uintptr {
	base := auxval(atSysinfoEhdr)
	if base == 0 || peek32(base) != 0x464C457F { // "\x7fELF"
		return 0
	}
	// The program headers give the load bias and the dynamic section.
	phoff := uintptr(peek64(base + 32))
	phentsize := uintptr(peek16(base + 54))
	phnum := int(peek16(base + 56))
	var bias, dyn uintptr
	haveLoad := false
	for i := 0; i < phnum; i++ {
		ph := base + phoff + uintptr(i)*phentsize
		off := uintptr(peek64(ph + 8))
		switch peek32(ph) {
		case 1: // PT_LOAD
			if !haveLoad {
				bias = base + off - uintptr(peek64(ph+16))
				haveLoad = true
			}
		case 2: // PT_DYNAMIC
			dyn = base + off
		}
	}
	if !haveLoad || dyn == 0 {
		return 0
	}
	var strtab, symtab, hash, gnuhash uintptr
	for d := dyn; ; d += 16 {
		tag := peek64(d)
		if tag == 0 {
			break
		}
		val := bias + uintptr(peek64(d+8))
		switch tag {
		case 4: // DT_HASH
			hash = val
		case 5: // DT_STRTAB
			strtab = val
		case 6: // DT_SYMTAB
			symtab = val
		case 0x6ffffef5: // DT_GNU_HASH
			gnuhash = val
		}
	}
	nsym := 0
	switch {
	case strtab == 0 || symtab == 0:
		return 0
	case hash != 0:
		nsym = int(peek32(hash + 4)) // nchain: one entry per symbol
	case gnuhash != 0:
		nsym = gnuSymCount(gnuhash)
	}
	for i := 0; i < nsym; i++ {
		sym := symtab + uintptr(i)*24 // Elf64_Sym
		info := peek8(sym + 4)
		bind := info >> 4
		if info&0xf != 2 || bind != 1 && bind != 2 || peek16(sym+6) == 0 {
			continue // not a defined global or weak function
		}
		if cstrIs(strtab+uintptr(peek32(sym)), name) {
			return bias + uintptr(peek64(sym+8))
		}
	}
	return 0
}

// gnuSymCount returns the number of symbols a DT_GNU_HASH table covers:
// one past the last symbol of the chain of the highest bucket.
func gnuSymCount(h uintptr) int {
	nbuckets := uintptr(peek32(h))
	symoffset := uintptr(peek32(h + 4))
	bloomSize := uintptr(peek32(h + 8))
	buckets := h + 16 + bloomSize*8
	chains := buckets + nbuckets*4
	last := uintptr(0)
	for i := uintptr(0); i < nbuckets; i++ {
		if b := uintptr(peek32(buckets + i*4)); b > last {
			last = b
		}
	}
	if last < symoffset {
		return int(symoffset)
	}
	for peek32(chains+(last-symoffset)*4)&1 == 0 {
		last++
	}
	return int(last + 1)
}

// cstrIs reports whether the NUL-terminated string at p is s.
func cstrIs(p uintptr, s string) bool {
	for i := 0; i < len(s); i++ {
		if peek8(p+uintptr(i)) != s[i] {
			return false
		}
	}
	return peek8(p+uintptr(len(s))) == 0
}
