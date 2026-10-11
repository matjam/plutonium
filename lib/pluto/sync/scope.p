package sync

import "pluto/rt"

// A Scope starts tasks that may use the variables of the function that
// starts them, and waits for all of them before its own block ends: when
// the block that declares it ends, on every path out of it (falling off
// the end, return, break, continue, goto, rt.Goexit), it waits as Wait
// does, before the block's other variables are dropped (language spec
// section 4.11).
//
//	var s sync.Scope
//	for i, it := range items {
//		s.Task(func() { results[i] = work(it) })
//	}
//	s.Wait() // optional: s waits when its block ends in any case
//
// A Scope has no Add or Done: only the runtime counts its tasks, from the
// moment Task starts one until its function has returned, so Wait
// returning proves that every task it started has returned. The zero Scope
// is ready to use. A Scope is an owner: it moves instead of being copied,
// and it may not be moved once it has started a task.
//
// Unlike a sync.WaitGroup, which waits only where the program calls Wait,
// a Scope declared in a function lets its tasks use that function's
// variables: the compiler checks that they do so without data races
// (spec section 6.11). A Scope reached through a pointer still waits when
// it is dropped, but the compiler cannot see where, so its tasks follow
// the rules of a go statement: f may not capture variables.
type Scope owner struct {
	j rt.Join
}

// Task starts f on a new task that s waits for.
func (s *Scope) Task(f func()) {
	// The Join is hidden from the escape analysis (rt.Pinned): s waits for
	// the task before the frame that holds it ends. A Scope the compiler
	// sees in the starting frame never gets here; its Task calls are
	// lowered in place (internal/lower/scope_task.go).
	rt.TaskJoined(rt.Pinned(&s.j), f)
}

// Wait returns when every task started by s has returned. The return of
// each task is synchronized before Wait returns (memory-model.md section
// 3.4).
func (s *Scope) Wait() {
	rt.JoinWait(rt.Pinned(&s.j))
}

// Drop waits as Wait does. It runs when s goes out of scope.
func (s *Scope) Drop() {
	rt.JoinWait(rt.Pinned(&s.j))
}
