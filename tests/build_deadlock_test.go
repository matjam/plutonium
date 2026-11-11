// Ported from internal/build/deadlock_test.go at the go-cutover tag.

package tests

import (
	"regexp"
	"strings"
	"testing"
)

// TestTraceTask checks rt.TraceTask: the trace of a parked task, taken
// from another, with the runtime's frames left out, and the wait reason.
func TestTraceTask(t *testing.T) {
	t.Parallel()
	runProgCases(t, []progCase{
		{name: "tracetask", want: `reason: chan receive
    main.inner (./main.p:14)
    main.blocked (./main.p:19)
    main.main.task1 (./main.p:31)
reason: sleep
`, src: `package main

import (
	"pluto/rt"
	"pluto/time"
)

// Each task sends its task record to main, which waits for both to
// park.

// inner and blocked are named in the trace of the task parked in
// inner, with the task statement's function at the bottom.
func inner(c chan int) {
	<-c
}

func blocked(c chan int, ready chan *rt.Task) {
	ready <- rt.CurrentTask()
	inner(c)
}

func asleep(ready chan *rt.Task) {
	ready <- rt.CurrentTask()
	rt.ParkUntil(rt.Now()+int64(time.Hour))
}

func main() {
	c := make(chan int)
	ready := make(chan *rt.Task)
	sleeping := make(chan *rt.Task)
	task blocked(c, ready)
	task asleep(sleeping)
	parked := <-ready
	sleeper := <-sleeping
	for !parked.Parked() || !sleeper.Parked() {
		rt.Yield()
	}
	println("reason:", parked.WaitReason().String())
	rt.TraceTask(parked, rt.Console{})
	println("reason:", sleeper.WaitReason().String())
	c <- 1
	rt.Ready(sleeper)
}
`},
	})
}

// Frames of the standard library carry its line numbers, which change as the
// library does; the deadlock report of tasks parked in pluto/sync is
// compared with them masked.
var libLine = regexp.MustCompile(`\((pluto/[a-z/]+\.p):[0-9]+\)`)

// TestDeadlockSync checks the deadlock report for tasks parked in
// pluto/sync, at -O0 and -O2, where inlining changes which frames the
// library's functions appear in: the same tasks, reasons and program
// frames.
func TestDeadlockSync(t *testing.T) {
	t.Parallel()
	src := `package main

import "pluto/sync"

var (
	mu sync.Mutex
	rw sync.RWMutex
	wg sync.WaitGroup
)

func lockTwice() {
	mu.Lock()
	mu.Lock()
}

func readLocked() {
	rw.RLock()
	rw.Lock()
}

func waitGroup() {
	wg.Wait()
}

func condition() {
	mu2 := sync.Mutex{}
	var c sync.Cond
	mu2.Lock()
	c.Wait(&mu2)
}

func main() {
	wg.Add(1)
	task lockTwice()
	task readLocked()
	task waitGroup()
	task condition()
	wg.Wait()
}
`
	for _, level := range []int{0, 2} {
		prog := buildConc(t, src, optFlag(level))
		got, status := runExeEnv(t, prog)
		got = libLine.ReplaceAllString(got, "($1:N)")
		const want = `fatal error: all tasks are asleep - deadlock!

task 1 [sync.WaitGroup.Wait]:
    pluto/sync.(*WaitGroup).Wait (pluto/sync/waitgroup.p:N)
    main.main (./main.p:38)

task 2 [sync.Mutex.Lock]:
    pluto/sync.(*Mutex).lockSlow (pluto/sync/mutex.p:N)
    pluto/sync.(*Mutex).Lock (pluto/sync/mutex.p:N)
    main.lockTwice (./main.p:13)
    main.main.task1 (./main.p:34)

task 3 [sync.RWMutex.Lock]:
    pluto/sync.(*sema).acquire (pluto/sync/mutex.p:N)
    pluto/sync.(*RWMutex).Lock (pluto/sync/rwmutex.p:N)
    main.readLocked (./main.p:18)
    main.main.task2 (./main.p:35)

task 4 [sync.WaitGroup.Wait]:
    pluto/sync.(*WaitGroup).Wait (pluto/sync/waitgroup.p:N)
    main.waitGroup (./main.p:22)
    main.main.task3 (./main.p:36)

task 5 [sync.Cond.Wait]:
    pluto/sync.(*Cond).Wait (pluto/sync/waitgroup.p:N)
    main.condition (./main.p:29)
    main.main.task4 (./main.p:37)
`
		if status != 2 || got != want {
			t.Errorf("O%d: status %d, output:\n%s\nwant status 2, output:\n%s", level, status, got, want)
		}
	}
}

// TestDeadlockPanicking checks that a task waiting for good in a deferred
// call that runs for a panic is reported with the panic (language spec
// sections 19.1 and 19.4, debugging.md section 5): a panicking: line with
// the message, then a trace that goes on from the deferred call to where
// the panic happened. A task that is not panicking has no such line.
func TestDeadlockPanicking(t *testing.T) {
	t.Parallel()
	src := `package main

import "pluto/sync"

func main() {
	release := make(chan int)
	var s sync.Scope
	defer s.Wait()
	s.Task(func() {
		<-release
	})
	panic("lost key")
}
`
	for _, level := range []int{0, 2} {
		prog := buildConc(t, src, optFlag(level))
		got, status := runExeEnv(t, prog)
		got = libLine.ReplaceAllString(got, "($1:N)")
		const want = `fatal error: all tasks are asleep - deadlock!

task 1 [sync.Scope.Wait]:
panicking: lost key
    pluto/sync.(*Scope).Wait (pluto/sync/scope.p:N)
    main.main (./main.p:12)
`
		if status != 2 || !strings.HasPrefix(got, want) || strings.Count(got, "panicking:") != 1 || !strings.Contains(got, "[chan receive]:\n") || strings.Contains(got, "pluto/rt.") {
			t.Errorf("O%d: status %d, output:\n%s\nwant status 2, output starting:\n%s", level, status, got, want)
		}
	}
}
