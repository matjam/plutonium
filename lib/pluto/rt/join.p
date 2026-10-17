package rt

import "unsafe"

// ----------------------------------------------------------------------------
// Joins: the tasks a sync.Scope starts (language spec section 19.4).
//
// A Join counts the tasks started for it that have not yet returned. Only
// the runtime changes the count: spawnScoped (or TaskJoined) counts a task
// before it starts, and taskMain uncounts it once its function has
// returned, so a Wait that returns proves that every task started for the
// Join has returned, whatever else the program does. A Join lives in the
// frame of the function that starts the tasks (sync.Scope), which waits
// for them before the frame ends; nothing here touches a Join after the
// last uncount has handed it back to its waiter.
//
// The task that starts scoped tasks keeps a chain of its Joins that may
// have tasks running, so that a kernel that abandons a task's stack (a
// task it kills without letting it return) can first wait for the tasks
// that may still use that stack: WaitScopes (abi.md section 12.10).

// A Join counts running tasks for one sync.Scope.
type Join struct {
	mu     Lock
	n      int64 // tasks started and not yet returned
	waiter *Task // the task parked in Wait, if any
	next   *Join // the owner's chain of Joins with tasks
	owner  *Task // the task whose chain holds the Join, or nil
}

// joinEnter counts a task about to start for j.
func joinEnter(j *Join) {
	j.mu.Lock()
	j.n++
	j.mu.Unlock()
}

// joinChain puts j on the running task's chain, if it is not on one: the
// task that started tasks for it may not leave the frame holding it until
// they have returned.
func joinChain(j *Join) {
	t := current()
	if t == nil || j.owner != nil {
		return
	}
	j.owner = t
	j.next = t.joins
	t.joins = j
}

// joinUnchain takes j off its owner's chain once it has no tasks. Only the
// owner changes its chain.
func joinUnchain(j *Join) {
	t := j.owner
	if t == nil || t != current() {
		return
	}
	for p := &t.joins; *p != nil; p = &(*p).next {
		if *p == j {
			*p = j.next
			break
		}
	}
	j.next = nil
	j.owner = nil
}

// joinExit uncounts a task of j whose function has returned, and wakes the
// waiter when it was the last. It touches nothing of j after the lock is
// released: the waiter may return and its frame be reused at once.
func joinExit(j *Join) {
	j.mu.Lock()
	j.n--
	var w *Task
	if j.n == 0 {
		w = j.waiter
		j.waiter = nil
	}
	j.mu.Unlock()
	if w != nil {
		Ready(w)
	}
}

// JoinWait parks until every task started for j has returned. A Join with
// no tasks returns at once, after one lock.
func JoinWait(j *Join) {
	j.mu.Lock()
	if j.n == 0 {
		j.mu.Unlock()
		joinUnchain(j)
		return
	}
	if j.waiter != nil {
		j.mu.Unlock()
		panicMessage("sync: two tasks wait on one Scope", "", "", "", "")
	}
	j.waiter = self()
	j.mu.Unlock()
	park(WaitScope)
	joinUnchain(j)
}

// joinMoved stops the program when a Scope moves while tasks it started
// may still run: they find its Join at the address it had, so nothing
// would wait for them where it went. The compiler rejects the moves it can
// see (E3405); this catches a Scope moved after a helper started tasks
// for it through a pointer.
func joinMoved(j *Join) {
	j.mu.Lock()
	n := j.n
	j.mu.Unlock()
	if n != 0 {
		panicMessage("sync: a Scope was moved while tasks it started may still run", "", "", "", "")
	}
}

// JoinRunning returns how many tasks started for j have not yet returned.
func JoinRunning(j *Join) int {
	j.mu.Lock()
	n := j.n
	j.mu.Unlock()
	return int(n)
}

// self returns the running task, which must exist to wait.
func self() *Task {
	startTasks()
	t := current()
	if t == nil {
		panicMessage("sync: wait outside a task", "", "", "", "")
	}
	return t
}

// spawnScoped implements s.Task(f) for a Scope the compiler can see in the
// starting frame: as spawnGo, but the new task is counted by j until its
// function returns. fn is the thunk's closure record and args its argument
// block (internal/lower/conc.go).
func spawnScoped(j *Join, fn, args unsafe.Pointer, argSize, argAlign, bound uintptr) {
	req := StackRequest{Size: DefaultStackSize, Unsized: true, Alloc: defaultAllocator}
	if bound != 0 {
		req.Size, req.Proven, req.Unsized = bound, true, false
	}
	joinEnter(j)
	joinChain(j)
	spawnFor(j, fn, args, argSize, argAlign, req)
}

// TaskJoined starts f on a new task counted by j, for a sync.Scope the
// compiler cannot see in the starting frame (one reached through a
// pointer): f must not capture variables, as for a task statement, and it
// panics if f's closure record lies in the calling task's stack.
func TaskJoined(j *Join, f func()) {
	checkGoFunc(*(*unsafe.Pointer)(unsafe.Pointer(&f)))
	joinEnter(j)
	rec := *(*unsafe.Pointer)(unsafe.Pointer(&f))
	spawnFor(j, rec, nil, 0, 1, StackRequest{Size: DefaultStackSize, Unsized: true, Alloc: defaultAllocator})
}

// WaitScopes parks until every task started for a Join on t's chain has
// returned, and empties the chain. A kernel calls it for a task whose stack
// it is about to free or reuse without the task having returned normally
// (abi.md section 12.10): scoped tasks may still use that stack. t must not
// be running.
func WaitScopes(t *Task) {
	for j := t.joins; j != nil; {
		next := j.next
		j.mu.Lock()
		if j.n > 0 {
			j.waiter = self()
			j.mu.Unlock()
			park(WaitScope)
		} else {
			j.mu.Unlock()
		}
		j.next = nil
		j.owner = nil
		j = next
	}
	t.joins = nil
}

// ----------------------------------------------------------------------------
// Drops in progress at exit (language spec section 15.4).
//
// When main returns the program exits without waiting for other tasks, as
// in Go, except that a drop another task has begun completes first: a Drop
// method that is running, or the release of the last reference to a
// counted object (its drop and the return of its memory). The compiler
// brackets each drop glue function and each last release with dropBegin
// and dropEnd; the task's count of drops in progress nests, and dropping
// counts the tasks inside one.

var (
	dropping    uint32 // tasks inside a drop
	exitWaiting uint32 // main has returned and waits for them
	exitLock    Lock
	exitWaiter  *Task
)

// dropBegin marks the running task as inside a drop.
func dropBegin() {
	t := current()
	if t == nil {
		return
	}
	t.drops++
	if t.drops == 1 && tasking {
		t.dropCounted = true
		xadd32(&dropping, 1)
	}
}

// dropEnd ends a drop dropBegin began, and wakes main if it waits for the
// last one.
func dropEnd() {
	t := current()
	if t == nil || t.drops == 0 {
		return
	}
	t.drops--
	if t.drops == 0 && t.dropCounted {
		dropDone(t)
	}
}

// dropDone uncounts t, which is no longer inside a drop. It touches only
// the count, so that drop glue, which interrupt handlers and the panic
// handler may run, calls nothing that could wait or that needs a scheduler:
// the hosted monitor wakes main once the count is zero (exitReady).
func dropDone(t *Task) {
	t.dropCounted = false
	xadd32(&dropping, -1)
}

// exitReady readies main if it waits for drops in progress and none is
// left. It reports whether main waits at all.
func exitReady() bool {
	if load32(&exitWaiting) == 0 {
		return false
	}
	if load32(&dropping) != 0 {
		return true
	}
	exitLock.Lock()
	w := exitWaiter
	exitWaiter = nil
	exitLock.Unlock()
	if w != nil {
		Ready(w)
	}
	return true
}

// waitDrops parks the main task, whose main function has returned, until
// no other task is inside a drop. The hosted scheduler's monitor readies it
// (exitReady).
func waitDrops() {
	store32(&exitWaiting, 1)
	exitLock.Lock()
	if load32(&dropping) == 0 {
		exitLock.Unlock()
		return
	}
	exitWaiter = current()
	exitLock.Unlock()
	park(WaitExit)
}

// panicTaskStep stops a program whose loop starts the tasks of a
// sync.Scope with a variable step that is not positive: its iterations
// would not have different values of the loop variable, which the
// compiler relies on to keep the tasks' parts of a slice apart (language
// spec section 19.4).
func panicTaskStep(n int) {
	startPanic()
	printString("runtime error: the step of a loop that starts tasks of a sync.Scope is not positive: ")
	printInt(int64(n))
	fatal(nil)
}

// panicTaskOverlap stops a program that starts the tasks of a sync.Scope
// with two slices, which the tasks use only in the same partition form
// (out[i] and in[i]), that overlap without starting at the same element:
// the tasks would use some elements twice (language spec section 19.4).
func panicTaskOverlap(a, b string) {
	startPanic()
	printString("runtime error: the slices ")
	printString(a)
	printString(" and ")
	printString(b)
	printString(", used by the tasks of a sync.Scope, overlap without starting at the same element")
	fatal(nil)
}

// panicTaskOverflow stops a program whose index arithmetic in a task of a
// sync.Scope, or whose loop step around one, overflows: the parts of a
// slice that the compiler proved apart would wrap onto each other
// (language spec section 19.4).
func panicTaskOverflow() {
	startPanic()
	printString("runtime error: integer overflow in an index of a task of a sync.Scope")
	fatal(nil)
}
