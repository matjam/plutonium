package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Channels and select (language spec section 4.8), after Go's
// implementation. They use only Park and Ready, so they work on any
// scheduler. A puroutine that blocks puts a waiting record (sudog) on the
// channel's queue; the record lives in the blocked puroutine's own frame
// (or, for select, in the select statement's case array), which stays put
// while it is parked.
//
// Entry points that may block carry //pu:blocking; the non-blocking ones
// (select with a default case, close, len, cap) never call a function that
// does, so a checker of interrupt handlers can tell them apart from the
// call graph.

// An hchan is a channel. A chan value is its address.
type hchan struct {
	lock     Lock
	qcount   uint // values in the buffer
	dataqsiz uint // the buffer's capacity
	buf      unsafe.Pointer
	elemsize uintptr
	closed   uint32
	sendx    uint
	recvx    uint
	recvq    waitq // puroutines waiting to receive
	sendq    waitq // puroutines waiting to send
	alloc    Allocator
	memSize  uintptr
	memAlign uintptr
}

type waitq struct {
	first *sudog
	last  *sudog
}

// A sudog is a puroutine waiting on a channel.
type sudog struct {
	g        *Task
	next     *sudog
	prev     *sudog
	elem     unsafe.Pointer // the value to send, or where to receive (may be nil)
	c        *hchan
	isSelect bool
	success  bool // the operation completed (false: the channel was closed)
}

// Case kinds of a select (scase.kind).
const (
	caseRecv = 1
	caseSend = 2
)

// An scase is one case of a select statement, laid out by the compiler in
// the selecting frame (internal/lower/conc.go reads its size from here).
type scase struct {
	c    *hchan
	elem unsafe.Pointer
	kind uintptr
	sg   sudog
}

// makechan implements make(chan T, size) from the default allocator.
func makechan(elemsize, elemalign uintptr, size int) unsafe.Pointer {
	a := DefaultAllocator()
	if a == nil {
		panicMessage("no allocator: call alloc.SetDefault before allocating", "", "", "", "")
	}
	return makechanAt(a, elemsize, elemalign, size, CallerPC())
}

// makechanWith implements make(chan T, size, a).
func makechanWith(a Allocator, elemsize, elemalign uintptr, size int) unsafe.Pointer {
	if a == nil {
		panicMessage("nil allocator passed to new or make", "", "", "", "")
	}
	return makechanAt(a, elemsize, elemalign, size, CallerPC())
}

func makechanAt(a Allocator, elemsize, elemalign uintptr, size int, pc uintptr) unsafe.Pointer {
	if size < 0 || elemsize > 0 && uintptr(size) > (uintptr(1)<<40)/elemsize {
		panicMessage("makechan: size out of range", "", "", "", "")
	}
	if elemalign < 8 {
		elemalign = 8
	}
	hdr := (unsafe.Sizeof(hchan{}) + elemalign - 1) &^ (elemalign - 1)
	total := hdr + elemsize*uintptr(size)
	p := allocAt(a, total, elemalign, pc)
	c := (*hchan)(p)
	c.buf = unsafe.Add(p, hdr)
	c.dataqsiz = uint(size)
	c.elemsize = elemsize
	c.alloc = a
	c.memSize = total
	c.memAlign = elemalign
	return p
}

// FreeChan frees a channel made by make(chan T, ...) and its buffer,
// returning them to the allocator they came from. No puroutine may use the
// channel afterwards; values still buffered are discarded. A channel that
// puroutines wait on cannot be freed. Channels are freed explicitly until
// the compiler counts their references (roadmap item 48); then the last
// reference frees them and this goes away. pluto/alloc.FreeChan is the
// typed form.
func FreeChan(p unsafe.Pointer) {
	if p == nil {
		return
	}
	c := (*hchan)(p)
	c.lock.Lock()
	if c.recvq.first != nil || c.sendq.first != nil {
		c.lock.Unlock()
		panicMessage("FreeChan of a channel that puroutines are waiting on", "", "", "", "")
	}
	c.lock.Unlock()
	a := c.alloc
	if TrackSites {
		SetAllocSite(CallerPC())
	}
	a.Free(p, c.memSize, c.memAlign)
	if TrackSites {
		SetAllocSite(0)
	}
}

func chanbuf(c *hchan, i uint) unsafe.Pointer {
	return unsafe.Add(c.buf, uintptr(i)*c.elemsize)
}

func (q *waitq) enqueue(sg *sudog) {
	sg.next = nil
	sg.prev = q.last
	if q.last != nil {
		q.last.next = sg
	} else {
		q.first = sg
	}
	q.last = sg
}

// dequeue removes and returns the first waiter that can still be woken: a
// select whose other case has already won is skipped.
func (q *waitq) dequeue() *sudog {
	for {
		sg := q.first
		if sg == nil {
			return nil
		}
		q.first = sg.next
		if q.first == nil {
			q.last = nil
		} else {
			q.first.prev = nil
		}
		sg.next = nil
		sg.prev = nil
		if sg.isSelect && !cas32(&sg.g.selectDone, 0, 1) {
			continue
		}
		return sg
	}
}

// remove takes sg out of q if it is still there.
func (q *waitq) remove(sg *sudog) {
	if sg.prev == nil && q.first != sg {
		return // already dequeued
	}
	if sg.prev != nil {
		sg.prev.next = sg.next
	} else {
		q.first = sg.next
	}
	if sg.next != nil {
		sg.next.prev = sg.prev
	} else {
		q.last = sg.prev
	}
	sg.next = nil
	sg.prev = nil
}

// trySend sends the value at ep on c, whose lock is held, if that can be
// done without waiting. It returns whether it did, and the puroutine to
// wake once the lock is released. A send on a closed channel panics.
func trySend(c *hchan, ep unsafe.Pointer) (bool, *Task) {
	if c.closed != 0 {
		return false, nil
	}
	if sg := c.recvq.dequeue(); sg != nil {
		if sg.elem != nil {
			memmove(sg.elem, ep, int(c.elemsize))
		}
		sg.success = true
		sg.g.param = unsafe.Pointer(sg)
		return true, sg.g
	}
	if c.qcount < c.dataqsiz {
		memmove(chanbuf(c, c.sendx), ep, int(c.elemsize))
		c.sendx++
		if c.sendx == c.dataqsiz {
			c.sendx = 0
		}
		c.qcount++
		return true, nil
	}
	return false, nil
}

// tryRecv receives from c, whose lock is held, into ep (unless ep is nil)
// if that can be done without waiting. It returns whether it did, whether
// a value was received (false: c is closed and empty), and the puroutine
// to wake once the lock is released.
func tryRecv(c *hchan, ep unsafe.Pointer) (bool, bool, *Task) {
	if sg := c.sendq.dequeue(); sg != nil {
		if c.dataqsiz == 0 {
			if ep != nil {
				memmove(ep, sg.elem, int(c.elemsize))
			}
		} else {
			// The buffer is full: take its head and put the sender's
			// value at its tail, which is the same slot.
			qp := chanbuf(c, c.recvx)
			if ep != nil {
				memmove(ep, qp, int(c.elemsize))
			}
			memmove(qp, sg.elem, int(c.elemsize))
			c.recvx++
			if c.recvx == c.dataqsiz {
				c.recvx = 0
			}
			c.sendx = c.recvx
		}
		sg.success = true
		sg.g.param = unsafe.Pointer(sg)
		return true, true, sg.g
	}
	if c.qcount > 0 {
		qp := chanbuf(c, c.recvx)
		if ep != nil {
			memmove(ep, qp, int(c.elemsize))
		}
		memclr(qp, int(c.elemsize))
		c.recvx++
		if c.recvx == c.dataqsiz {
			c.recvx = 0
		}
		c.qcount--
		return true, true, nil
	}
	if c.closed != 0 {
		if ep != nil {
			memclr(ep, int(c.elemsize))
		}
		return true, false, nil
	}
	return false, false, nil
}

func panicSendClosed() {
	panicMessage("send on closed channel", "", "", "", "")
}

// chansend1 implements ch <- v: ep points at v.
//
//pu:blocking
func chansend1(p, ep unsafe.Pointer) {
	c := (*hchan)(p)
	if c == nil {
		parkForever()
	}
	c.lock.Lock()
	if c.closed != 0 {
		c.lock.Unlock()
		panicSendClosed()
	}
	ok, wake := trySend(c, ep)
	if ok {
		c.lock.Unlock()
		if wake != nil {
			Ready(wake)
		}
		return
	}
	sendWait(c, ep)
}

// sendWait blocks until a receiver takes the value at ep, with c's lock
// held on entry.
//
//pu:blocking
func sendWait(c *hchan, ep unsafe.Pointer) {
	var sg sudog
	sg.g = current()
	sg.elem = ep
	sg.c = c
	c.sendq.enqueue(&sg)
	c.lock.Unlock()
	Park()
	if !sg.success {
		panicSendClosed()
	}
}

// chanrecv1 implements <-ch, storing the value at ep (unless ep is nil).
//
//pu:blocking
func chanrecv1(p, ep unsafe.Pointer) {
	chanrecv2(p, ep)
}

// chanrecv2 implements v, ok := <-ch.
//
//pu:blocking
func chanrecv2(p, ep unsafe.Pointer) bool {
	c := (*hchan)(p)
	if c == nil {
		parkForever()
	}
	c.lock.Lock()
	done, received, wake := tryRecv(c, ep)
	if done {
		c.lock.Unlock()
		if wake != nil {
			Ready(wake)
		}
		return received
	}
	return recvWait(c, ep)
}

// recvWait blocks until a sender or close completes the receive, with c's
// lock held on entry.
//
//pu:blocking
func recvWait(c *hchan, ep unsafe.Pointer) bool {
	var sg sudog
	sg.g = current()
	sg.elem = ep
	sg.c = c
	c.recvq.enqueue(&sg)
	c.lock.Unlock()
	Park()
	return sg.success
}

// parkForever blocks the running puroutine for good: an operation on a nil
// channel, or select {}.
//
//pu:blocking
func parkForever() {
	for {
		Park()
	}
}

// closechan implements close(ch). It never blocks.
func closechan(p unsafe.Pointer) {
	c := (*hchan)(p)
	if c == nil {
		panicMessage("close of nil channel", "", "", "", "")
	}
	c.lock.Lock()
	if c.closed != 0 {
		c.lock.Unlock()
		panicMessage("close of closed channel", "", "", "", "")
	}
	c.closed = 1
	// Every waiter is released: receivers get the zero value, senders
	// panic. They are collected first and woken once the lock is free.
	var list *sudog
	for {
		sg := c.recvq.dequeue()
		if sg == nil {
			break
		}
		if sg.elem != nil {
			memclr(sg.elem, int(c.elemsize))
		}
		sg.success = false
		sg.g.param = unsafe.Pointer(sg)
		sg.next = list
		list = sg
	}
	for {
		sg := c.sendq.dequeue()
		if sg == nil {
			break
		}
		sg.success = false
		sg.g.param = unsafe.Pointer(sg)
		sg.next = list
		list = sg
	}
	c.lock.Unlock()
	for list != nil {
		sg := list
		list = sg.next
		Ready(sg.g) // sg is not touched after this: its puroutine may run
	}
}

// chanlen and chancap implement len(ch) and cap(ch).
func chanlen(p unsafe.Pointer) int {
	if p == nil {
		return 0
	}
	return int((*hchan)(p).qcount)
}

func chancap(p unsafe.Pointer) int {
	if p == nil {
		return 0
	}
	return int((*hchan)(p).dataqsiz)
}

// ----------------------------------------------------------------------------
// select

// selectgo implements a select statement without a default case. cases
// points at n scases and order at 2n uint16s of scratch space, both in the
// selecting frame. It returns the index of the case that proceeded and,
// for a receive, whether a value was received.
//
//pu:blocking
func selectgo(cases unsafe.Pointer, order *uint16, n int) (int, bool) {
	if n == 0 {
		parkForever()
	}
	g := current()
	scs := unsafe.Slice((*scase)(cases), n)
	pollorder := unsafe.Slice(order, n)
	lockorder := unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(order), 2*n)), n)
	norder := selectOrder(g, scs, pollorder, lockorder)
	selLock(scs, lockorder, norder)
	idx, ok, wake := selectPoll(scs, pollorder, lockorder, norder)
	if idx >= 0 {
		selUnlock(scs, lockorder, norder)
		if wake != nil {
			Ready(wake)
		}
		return idx, ok
	}
	if norder == 0 {
		parkForever()
	}
	return selectWait(g, scs, lockorder, norder)
}

// selectnb implements a select statement with a default case: it returns
// -1 if no case can proceed at once. It never blocks.
func selectnb(cases unsafe.Pointer, order *uint16, n int) (int, bool) {
	if n == 0 {
		return -1, false
	}
	g := current()
	scs := unsafe.Slice((*scase)(cases), n)
	pollorder := unsafe.Slice(order, n)
	lockorder := unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(order), 2*n)), n)
	norder := selectOrder(g, scs, pollorder, lockorder)
	selLock(scs, lockorder, norder)
	idx, ok, wake := selectPoll(scs, pollorder, lockorder, norder)
	selUnlock(scs, lockorder, norder)
	if wake != nil {
		Ready(wake)
	}
	return idx, ok
}

// selectOrder fills pollorder with a random permutation of the cases on
// non-nil channels, and lockorder with the same cases sorted by channel
// address, and returns how many there are.
func selectOrder(g *Task, scs []scase, pollorder, lockorder []uint16) int {
	norder := 0
	var r uint64
	if g != nil {
		r = fastrand(g)
	}
	for i := range scs {
		if scs[i].c == nil {
			continue
		}
		var j int
		if g != nil {
			j = int(fastrand(g) % uint64(norder+1))
		} else {
			r = r*6364136223846793005 + 1442695040888963407
			j = int((r >> 33) % uint64(norder+1))
		}
		pollorder[norder] = pollorder[j]
		pollorder[j] = uint16(i)
		norder++
	}
	// Insertion sort by channel address: selects are small.
	for i := 0; i < norder; i++ {
		k := pollorder[i]
		c := uintptr(unsafe.Pointer(scs[k].c))
		j := i
		for j > 0 && uintptr(unsafe.Pointer(scs[lockorder[j-1]].c)) > c {
			lockorder[j] = lockorder[j-1]
			j--
		}
		lockorder[j] = k
	}
	return norder
}

func selLock(scs []scase, lockorder []uint16, norder int) {
	var last *hchan
	for i := 0; i < norder; i++ {
		c := scs[lockorder[i]].c
		if c != last {
			c.lock.Lock()
			last = c
		}
	}
}

func selUnlock(scs []scase, lockorder []uint16, norder int) {
	for i := norder - 1; i >= 0; i-- {
		c := scs[lockorder[i]].c
		if i > 0 && scs[lockorder[i-1]].c == c {
			continue // locked once
		}
		c.lock.Unlock()
	}
}

// selectPoll looks for a case that can proceed at once, in poll order,
// with every channel locked, and performs it. It returns the case's index
// (or -1), whether a receive got a value, and a puroutine to wake once the
// locks are released.
func selectPoll(scs []scase, pollorder, lockorder []uint16, norder int) (int, bool, *Task) {
	for i := 0; i < norder; i++ {
		k := int(pollorder[i])
		cs := &scs[k]
		if cs.kind == caseRecv {
			done, received, wake := tryRecv(cs.c, cs.elem)
			if done {
				return k, received, wake
			}
		} else {
			if cs.c.closed != 0 {
				selUnlock(scs, lockorder, norder)
				panicSendClosed()
			}
			done, wake := trySend(cs.c, cs.elem)
			if done {
				return k, false, wake
			}
		}
	}
	return -1, false, nil
}

// selectWait waits on every case at once, with every channel locked on
// entry, and returns the case that a sender, receiver or close completed.
//
//pu:blocking
func selectWait(g *Task, scs []scase, lockorder []uint16, norder int) (int, bool) {
	store32(&g.selectDone, 0)
	g.param = nil
	for i := 0; i < norder; i++ {
		cs := &scs[lockorder[i]]
		sg := &cs.sg
		sg.g = g
		sg.elem = cs.elem
		sg.c = cs.c
		sg.isSelect = true
		sg.success = false
		if cs.kind == caseRecv {
			cs.c.recvq.enqueue(sg)
		} else {
			cs.c.sendq.enqueue(sg)
		}
	}
	selUnlock(scs, lockorder, norder)
	Park()
	selLock(scs, lockorder, norder)
	won := (*sudog)(g.param)
	idx := -1
	ok := false
	for i := 0; i < norder; i++ {
		k := int(lockorder[i])
		cs := &scs[k]
		sg := &cs.sg
		if sg == won {
			idx = k
			ok = sg.success
			continue
		}
		if cs.kind == caseRecv {
			cs.c.recvq.remove(sg)
		} else {
			cs.c.sendq.remove(sg)
		}
	}
	selUnlock(scs, lockorder, norder)
	if idx >= 0 && scs[idx].kind == caseSend && !ok {
		panicSendClosed()
	}
	return idx, ok
}
