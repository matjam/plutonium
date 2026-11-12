// Package handoff gives each task of a sync.Scope a *T of its own, out of
// sight of the race check (language spec section 19.3, What is not
// checked): the one place where the compiler's tasks reach memory through
// unsafe.Pointer to share it.
//
// It is sound because of how it is used, which the race check cannot see:
//
//   - each Ptr made by Of is given to one task of a Scope, and the pointers
//     of a Scope's tasks are distinct;
//   - the task owns what its pointer points to until the Scope ends: no
//     other task, the starting one included, uses it meanwhile;
//   - what the pointed-to value reaches that other tasks reach too (a
//     table they share, the round of work they take tasks from) is behind
//     a lock (rt.Lock) or is not written while the tasks run.
//
// The Scope's join orders everything the task wrote before what the
// starting task does after it (memory-model.md section 3.4). A use that
// breaks these rules is a data race the compiler does not report.
package handoff

import "unsafe"

// A Ptr is a *T held as an unsafe.Pointer, which the race check does not
// follow.
type Ptr[T owner any] struct {
	p unsafe.Pointer
}

// Of returns p as a Ptr, for one task.
func Of[T owner any](p *T) Ptr[T] { return Ptr[T]{p: unsafe.Pointer(p)} }

// Get returns the pointer.
func (h Ptr[T]) Get() *T { return (*T)(h.p) }
