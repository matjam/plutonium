//pu:build linux

package rt

import "unsafe"

// ----------------------------------------------------------------------------
// The poller
//
// One epoll instance and one thread blocked in epoll_wait serve every
// descriptor. It follows Go's pollDesc: a descriptor is registered once,
// for reading and writing, with edge-triggered notification; each has a
// PollDesc that holds the puroutines waiting to read, those waiting to
// write, and whether an event has arrived with nobody waiting. A read and a
// write may wait at the same time. Closing a descriptor evicts its
// waiters (PollDesc.Evict).
//
// A PollDesc is never returned to the system, only to a free list, so that
// the poller thread can still touch one that was released after it read the
// event. A wakeup for a descriptor that was released and reused is then a
// spurious one, which every user tolerates: a wait is always followed by
// trying the operation again.

const (
	epollIn      = 0x1
	epollOut     = 0x4
	epollErr     = 0x8
	epollHup     = 0x10
	epollRdhup   = 0x2000
	epollET      = 0x80000000
	epollCtlAdd  = 1
	epollCtlDel  = 2
	epollCloexec = 0x80000
)

// Results of PollDesc.Wait.
const (
	PollReady       = 0 // the descriptor may be ready: try the operation again
	PollClosed      = 1 // the descriptor was closed while waiting, or before
	PollUnavailable = 2 // there is no poller (a program's own scheduler)
)

var poller struct {
	lock    Lock
	started bool
	fd      int

	// Timers (timer_linux.p).
	startedFlag uint32 // started, read without the lock
	wakefd      int    // an eventfd in the epoll set, written to wake the poller early
	until       uint64 // the deadline the poller sleeps until, or NoDeadline while it looks for one
}

// A pollWaiter lives in the frame of the puroutine it describes, which is
// parked while the record is on a queue.
type pollWaiter struct {
	g    *Task
	next uintptr // *pollWaiter
}

// A PollDesc is the poller's record of one descriptor, made with PollOpen.
type PollDesc struct {
	lock       Lock
	fd         int
	registered bool
	closing    bool
	rready     bool    // a read event arrived while nobody waited
	wready     bool    // a write event arrived while nobody waited
	rq         uintptr // *pollWaiter: puroutines waiting to read
	wq         uintptr // *pollWaiter: puroutines waiting to write
	link       *PollDesc
}

var pollPool struct {
	lock Lock
	free *PollDesc
}

// PollOpen returns the record for fd, which must be in non-blocking mode.
// The descriptor is registered with the poller by the first Wait. Release
// the record before closing the descriptor.
func PollOpen(fd int) *PollDesc {
	pollPool.lock.Lock()
	pd := pollPool.free
	if pd == nil {
		const page = 4096
		n := page / int(unsafe.Sizeof(PollDesc{}))
		base := mustMmap(page)
		for i := n - 1; i >= 1; i-- {
			d := (*PollDesc)(unsafe.Add(base, uintptr(i)*unsafe.Sizeof(PollDesc{})))
			d.link = pollPool.free
			pollPool.free = d
		}
		pd = (*PollDesc)(base)
	} else {
		pollPool.free = pd.link
		pd.link = nil
	}
	pollPool.lock.Unlock()
	pd.fd = fd
	return pd
}

// Release deregisters the descriptor and returns the record to the free
// list. No puroutine may be waiting or about to wait on it: pluto/os calls
// it when the last operation on a closed file has finished.
func (pd *PollDesc) Release() {
	pd.lock.Lock()
	if pd.registered {
		syscall6(sysEpollCtl, uintptr(poller.fd), epollCtlDel, uintptr(pd.fd), 0, 0, 0)
	}
	pd.fd = -1
	pd.registered = false
	pd.closing = false
	pd.rready = false
	pd.wready = false
	pd.rq = 0
	pd.wq = 0
	pd.lock.Unlock()
	pollPool.lock.Lock()
	pd.link = pollPool.free
	pollPool.free = pd
	pollPool.lock.Unlock()
}

// Wait parks the running puroutine until the descriptor is ready for
// reading (or for writing, if write is set) and returns PollReady. It
// returns at once when an event arrived since the last wait, and the
// descriptor may still not be ready (the event may be stale): the caller
// tries its operation again and waits again if it would still block. Any
// number of puroutines may wait to read and any number to write; an event
// wakes all those waiting for it. PollClosed is returned if Evict was
// called, whether before or during the wait; PollUnavailable if there is
// no poller.
func (pd *PollDesc) Wait(write bool) int {
	startTasks()
	g := current()
	if !hs.started || g == nil {
		return PollUnavailable
	}
	startPoller()
	pd.lock.Lock()
	if pd.closing {
		pd.lock.Unlock()
		return PollClosed
	}
	if !pd.registered {
		var ev [12]byte // struct epoll_event, packed: events, then data
		*(*uint32)(unsafe.Pointer(&ev[0])) = epollIn | epollOut | epollRdhup | epollET
		*(*uintptr)(unsafe.Pointer(&ev[4])) = uintptr(unsafe.Pointer(pd))
		if r := syscall6(sysEpollCtl, uintptr(poller.fd), epollCtlAdd, uintptr(pd.fd), uintptr(unsafe.Pointer(&ev[0])), 0, 0); r != 0 {
			pd.lock.Unlock()
			return PollUnavailable // a regular file, say, which epoll refuses
		}
		pd.registered = true
	}
	var w pollWaiter
	w.g = g
	if write {
		if pd.wready {
			pd.wready = false
			pd.lock.Unlock()
			return PollReady
		}
		w.next = pd.wq
		pd.wq = uintptr(unsafe.Pointer(&w))
	} else {
		if pd.rready {
			pd.rready = false
			pd.lock.Unlock()
			return PollReady
		}
		w.next = pd.rq
		pd.rq = uintptr(unsafe.Pointer(&w))
	}
	xadd32(&hs.pollWaiting, 1)
	pd.lock.Unlock()
	ParkReason(WaitIO)
	pd.lock.Lock()
	closing := pd.closing
	pd.lock.Unlock()
	if closing {
		return PollClosed
	}
	return PollReady
}

// Evict wakes every puroutine waiting on the descriptor, and makes this and
// later waits return PollClosed. pluto/os calls it when a file is closed.
func (pd *PollDesc) Evict() {
	pd.lock.Lock()
	pd.closing = true
	wakeWaiters(&pd.rq)
	wakeWaiters(&pd.wq)
	pd.lock.Unlock()
}

// ready records an event from the poller thread: it wakes the puroutines
// waiting for it, or remembers it for the next wait.
func (pd *PollDesc) ready(read, write bool) {
	pd.lock.Lock()
	if read {
		if pd.rq != 0 {
			wakeWaiters(&pd.rq)
		} else {
			pd.rready = true
		}
	}
	if write {
		if pd.wq != 0 {
			wakeWaiters(&pd.wq)
		} else {
			pd.wready = true
		}
	}
	pd.lock.Unlock()
}

// wakeWaiters readies every puroutine on the queue and empties it. The pd's
// lock is held. A waiter's record is gone once it runs, so each next
// pointer is read before its puroutine is readied.
func wakeWaiters(q *uintptr) {
	w := (*pollWaiter)(unsafe.Pointer(*q))
	*q = 0
	for w != nil {
		next := (*pollWaiter)(unsafe.Pointer(w.next))
		Ready(w.g)
		xadd32(&hs.pollWaiting, -1)
		w = next
	}
}

func startPoller() {
	if load32(&poller.startedFlag) != 0 {
		return
	}
	poller.lock.Lock()
	if !poller.started {
		poller.fd = int(syscall3(sysEpollCreate1, epollCloexec, 0, 0))
		// The eventfd wakes the poller when a timer earlier than its
		// deadline is armed (timerEarlier). Its events carry no PollDesc.
		poller.wakefd = int(syscall3(sysEventfd2, 0, efdCloexec|efdNonblock, 0))
		var ev [12]byte
		*(*uint32)(unsafe.Pointer(&ev[0])) = epollIn
		syscall6(sysEpollCtl, uintptr(poller.fd), epollCtlAdd, uintptr(poller.wakefd), uintptr(unsafe.Pointer(&ev[0])), 0, 0)
		store64(&poller.until, uint64(NoDeadline))
		poller.started = true
		store32(&poller.startedFlag, 1)
		newThread(pollerMain, nil, false)
	}
	poller.lock.Unlock()
}

func pollerMain(mp *m) {
	mp.tid = int(syscall3(sysGettid, 0, 0, 0))
	var evs [64 * 12]byte
	for {
		n := pollerWait(&evs, pollTimers())
		for i := 0; i < n; i++ {
			events := *(*uint32)(unsafe.Pointer(&evs[i*12]))
			pd := (*PollDesc)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(&evs[i*12+4]))))
			if pd == nil {
				var buf [8]byte
				syscall3(sysRead, uintptr(poller.wakefd), uintptr(unsafe.Pointer(&buf[0])), 8)
				continue
			}
			const fail = epollErr | epollHup
			pd.ready(events&(epollIn|epollRdhup|fail) != 0, events&(epollOut|fail) != 0)
		}
	}
}
