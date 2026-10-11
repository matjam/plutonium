// Package sync provides mutual exclusion locks, wait groups, one-time
// initialisation and condition variables, as Go's sync package does
// (language spec section F, memory-model.md section 3.4).
//
// They are built on the runtime's two scheduler operations, rt.Park and
// rt.Ready, so they work on any scheduler, the hosted one or a kernel's. A
// task that has to wait parks; it never spins its processor for long.
// The operations that may park (Lock, RLock, Wait, Once.Do, WaitGroup.Task)
// must not be used in interrupt context, which the compiler checks by
// following the calls of interrupt handlers into rt.Park; Unlock, TryLock,
// Signal, Broadcast, Add and Done never park.
//
// As in Go, none of these types may be copied after first use, and their
// zero values are ready to use. A program that waits on a value in a frame
// must not let another task reach it after the frame returns: allocate
// it, or make it a package-level variable (language spec section 18.3).
package sync

import "pluto/rt"

// A Locker represents an object that can be locked and unlocked.
type Locker interface {
	Lock()
	Unlock()
}

// A waiter is a parked task in a waitQueue. It lives in the parked
// task's frame.
type waiter struct {
	t    *rt.Task
	next *waiter
}

// A waitQueue is a FIFO of waiters, guarded by the lock of whatever holds
// it.
type waitQueue struct {
	head, tail *waiter
}

func (q *waitQueue) push(w *waiter) {
	w.next = nil
	if q.tail != nil {
		q.tail.next = w
	} else {
		q.head = w
	}
	q.tail = w
}

// pushFront puts w first, for a waiter that was woken and lost the race
// for the lock it waited for.
func (q *waitQueue) pushFront(w *waiter) {
	w.next = q.head
	q.head = w
	if q.tail == nil {
		q.tail = w
	}
}

func (q *waitQueue) pop() *waiter {
	w := q.head
	if w != nil {
		q.head = w.next
		if q.head == nil {
			q.tail = nil
		}
		w.next = nil
	}
	return w
}

func (q *waitQueue) empty() bool { return q.head == nil }

// readyAll wakes every waiter in the list starting at w. A woken waiter's
// record may vanish as soon as it runs, so its successor is read first.
func readyAll(w *waiter) {
	for w != nil {
		next := w.next
		rt.Ready(w.t)
		w = next
	}
}

// fail stops the program with a sync error, as Go's runtime does.
func fail(msg string) {
	panic(msg)
}

// self returns the running task, which must exist to wait.
func self() *rt.Task {
	t := rt.CurrentTask()
	if t == nil {
		fail("sync: wait outside a task")
	}
	return t
}
