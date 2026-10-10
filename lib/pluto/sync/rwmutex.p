package sync

import (
	"pluto/rt"
	"pluto/sync/atomic"
)

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// The algorithm is Go's sync.RWMutex.

const rwmutexMaxReaders = 1 << 30

// An RWMutex is a reader/writer mutual exclusion lock: held by any number
// of readers or by one writer. The zero value is unlocked.
//
// A writer that calls Lock waits for the readers that hold the lock, and
// keeps new readers out until it has had its turn, so writers do not
// starve. The guarantees are Go's (memory-model.md section 3.4).
type RWMutex struct {
	w           Mutex // held by a writer that holds or waits for the lock
	writerSem   sema  // a writer waits here for the readers to leave
	readerSem   sema  // readers wait here for a writer to finish
	readerCount int32 // readers holding or waiting; negative while a writer is pending
	readerWait  int32 // readers the pending writer still waits for
}

// RLock locks rw for reading.
func (rw *RWMutex) RLock() {
	if atomic.AddInt32(&rw.readerCount, 1) < 0 {
		// A writer is pending: wait for it.
		rw.readerSem.acquire(rt.WaitRWMutexRead)
	}
}

// TryRLock locks rw for reading if no writer holds or waits for it, and
// reports whether it did.
func (rw *RWMutex) TryRLock() bool {
	for {
		c := atomic.LoadInt32(&rw.readerCount)
		if c < 0 {
			return false
		}
		if atomic.CompareAndSwapInt32(&rw.readerCount, c, c+1) {
			return true
		}
	}
}

// RUnlock undoes one RLock.
func (rw *RWMutex) RUnlock() {
	if r := atomic.AddInt32(&rw.readerCount, -1); r < 0 {
		rw.rUnlockSlow(r)
	}
}

func (rw *RWMutex) rUnlockSlow(r int32) {
	if r+1 == 0 || r+1 == -rwmutexMaxReaders {
		fail("sync: RUnlock of unlocked RWMutex")
	}
	// A writer is pending: the last reader out lets it in.
	if atomic.AddInt32(&rw.readerWait, -1) == 0 {
		rw.writerSem.release()
	}
}

// Lock locks rw for writing.
func (rw *RWMutex) Lock() {
	rw.w.Lock()
	// Tell readers a writer is pending.
	r := atomic.AddInt32(&rw.readerCount, -rwmutexMaxReaders) + rwmutexMaxReaders
	// Wait for the readers that hold the lock.
	if r != 0 && atomic.AddInt32(&rw.readerWait, r) != 0 {
		rw.writerSem.acquire(rt.WaitRWMutexWrite)
	}
}

// TryLock locks rw for writing if it is free and reports whether it did.
func (rw *RWMutex) TryLock() bool {
	if !rw.w.TryLock() {
		return false
	}
	if !atomic.CompareAndSwapInt32(&rw.readerCount, 0, -rwmutexMaxReaders) {
		rw.w.Unlock()
		return false
	}
	return true
}

// Unlock undoes Lock.
func (rw *RWMutex) Unlock() {
	r := atomic.AddInt32(&rw.readerCount, rwmutexMaxReaders)
	if r >= rwmutexMaxReaders {
		fail("sync: Unlock of unlocked RWMutex")
	}
	// Let in the readers that waited.
	for i := 0; i < int(r); i++ {
		rw.readerSem.release()
	}
	rw.w.Unlock()
}

// RLocker returns a Locker whose Lock and Unlock are rw's RLock and
// RUnlock.
func (rw *RWMutex) RLocker() Locker { return (*rlocker)(rw) }

type rlocker RWMutex

func (r *rlocker) Lock()   { (*RWMutex)(r).RLock() }
func (r *rlocker) Unlock() { (*RWMutex)(r).RUnlock() }
