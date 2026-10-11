package build_test

import (
	"regexp"
	"testing"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestTraceTask checks rt.TraceTask: the trace of a parked puroutine, taken
// from another, with the runtime's frames left out, and the wait reason.
func TestTraceTask(t *testing.T) {
	testguard.Parallel(t)
	runConcCases(t, []concCase{
		{name: "tracetask", want: `reason: chan receive
    main.inner (./main.p:14)
    main.blocked (./main.p:19)
    main.main.go1 (./main.p:31)
reason: sleep
`, src: `package main

import (
	"pluto/rt"
	"pluto/time"
)

// Each puroutine sends its task record to main, which waits for both to
// park.

// inner and blocked are named in the trace of the puroutine parked in
// inner, with the go statement's function at the bottom.
func inner(c chan int) {
	<-c
}

func blocked(c chan int, ready chan *rt.Task) {
	ready <- rt.CurrentTask()
	inner(c)
}

func asleep(ready chan *rt.Task) {
	ready <- rt.CurrentTask()
	time.Sleep(time.Hour)
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
}
`},
	}, build.Config{})
}

// Frames of the standard library carry its line numbers, which change as the
// library does; the deadlock report of puroutines parked in pluto/sync is
// compared with them masked.
var libLine = regexp.MustCompile(`\((pluto/[a-z/]+\.p):[0-9]+\)`)

// TestDeadlockSync checks the deadlock report for puroutines parked in
// pluto/sync, at -O0 and -O2, where inlining changes which frames the
// library's functions appear in: the same puroutines, reasons and program
// frames.
func TestDeadlockSync(t *testing.T) {
	testguard.Parallel(t)
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
	c := sync.Cond{L: &mu2}
	mu2.Lock()
	c.Wait()
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
		prog := buildConc(t, src, build.Config{OptLevel: level})
		got, status := runConc(t, prog)
		got = libLine.ReplaceAllString(got, "($1:N)")
		const want = `fatal error: all puroutines are asleep - deadlock!

puroutine 1 [sync.WaitGroup.Wait]:
    pluto/sync.(*WaitGroup).Wait (pluto/sync/waitgroup.p:N)
    main.main (./main.p:38)

puroutine 2 [sync.Mutex.Lock]:
    pluto/sync.(*Mutex).lockSlow (pluto/sync/mutex.p:N)
    pluto/sync.(*Mutex).Lock (pluto/sync/mutex.p:N)
    main.lockTwice (./main.p:13)
    main.main.go1 (./main.p:34)

puroutine 3 [sync.RWMutex.Lock]:
    pluto/sync.(*sema).acquire (pluto/sync/mutex.p:N)
    pluto/sync.(*RWMutex).Lock (pluto/sync/rwmutex.p:N)
    main.readLocked (./main.p:18)
    main.main.go2 (./main.p:35)

puroutine 4 [sync.WaitGroup.Wait]:
    pluto/sync.(*WaitGroup).Wait (pluto/sync/waitgroup.p:N)
    main.waitGroup (./main.p:22)
    main.main.go3 (./main.p:36)

puroutine 5 [sync.Cond.Wait]:
    pluto/sync.(*Cond).Wait (pluto/sync/waitgroup.p:N)
    main.condition (./main.p:29)
    main.main.go4 (./main.p:37)
`
		if status != 2 || got != want {
			t.Errorf("O%d: status %d, output:\n%s\nwant status 2, output:\n%s", level, status, got, want)
		}
	}
}
