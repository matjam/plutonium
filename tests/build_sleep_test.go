// Ported from internal/build/sleep_test.go at the go-cutover tag.

package tests

import (
	"bufio"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// schedulerSymbols are functions that exist only in a program that starts
// the hosted scheduler: starting it, its monitor thread, and the poller.
var schedulerSymbols = []string{"pluto/rt.startTasks", "pluto/rt.startHosted", "pluto/rt.sysmonMain", "pluto/rt.startPoller"}

// symbolsOf returns which of names the executable prog defines.
func symbolsOf(t *testing.T, prog string, names []string) []string {
	t.Helper()
	f, err := elf.Open(prog)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	syms, err := f.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, s := range syms {
		for _, n := range names {
			if s.Name == n {
				found = append(found, n)
			}
		}
	}
	return found
}

// sleepThreads runs prog, which prints a line on stderr and then sleeps for
// a while, and returns the number of threads it has while it sleeps, with
// its output after it has exited.
func sleepThreads(t *testing.T, prog string) (threads int, out string) {
	t.Helper()
	cmd := exec.Command(prog)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(stderr)
	first, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the program's first line: %v", err)
	}
	// The program sleeps for 500ms after printing; read its tasks well
	// within that.
	time.Sleep(100 * time.Millisecond)
	ents, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("%v\n%s%s", err, first, rest)
	}
	return len(ents), first + string(rest)
}

// TestSleepWithoutScheduler checks that a program that only sleeps waits in
// the kernel on its one thread, and links neither the scheduler's start nor
// its monitor and poller threads, while a program that also uses tasks,
// channels or timers keeps the scheduler and parks (language spec section
// 19.5).
func TestSleepWithoutScheduler(t *testing.T) {
	t.Parallel()
	const check = `
	println("elapsed ok", time.Since(start) >= 500*time.Millisecond && time.Since(start) < 5*time.Second)
`
	cases := []struct {
		name, src string
		sched     bool // the program starts the scheduler
	}{
		{name: "sleeponly", src: `package main

import "pluto/time"

func main() {
	println("sleeping")
	start := time.Now()
	time.Sleep(500 * time.Millisecond)
	time.Sleep(0)
	time.Sleep(-time.Second)` + check + `}
`},
		// Sleep reached through another function.
		{name: "nested", src: `package main

import "pluto/time"

func nap(d time.Duration) { time.Sleep(d) }

func main() {
	println("sleeping")
	start := time.Now()
	nap(500 * time.Millisecond)` + check + `}
`},
		{name: "go", sched: true, src: `package main

import "pluto/time"

func child() { time.Sleep(time.Millisecond) }

func main() {
	println("sleeping")
	start := time.Now()
	task child()
	time.Sleep(500 * time.Millisecond)` + check + `}
`},
		{name: "chan", sched: true, src: `package main

import "pluto/time"

func main() {
	println("sleeping")
	start := time.Now()
	c := make(chan int, 1)
	c <- 1
	<-c
	select {
	case v := <-c:
		println(v)
	default:
	}
	time.Sleep(500 * time.Millisecond)` + check + `}
`},
		{name: "blockingchan", sched: true, src: `package main

import "pluto/time"

func send(c chan int) { c <- 1 }

func main() {
	println("sleeping")
	start := time.Now()
	c := make(chan int)
	task send(c)
	<-c
	time.Sleep(500 * time.Millisecond)` + check + `}
`},
		{name: "timer", sched: true, src: `package main

import "pluto/time"

func main() {
	println("sleeping")
	start := time.Now()
	t := time.NewTimer(time.Hour)
	t.Stop()
	time.Sleep(500 * time.Millisecond)` + check + `}
`},
		{name: "after", sched: true, src: `package main

import "pluto/time"

func main() {
	println("sleeping")
	start := time.Now()
	<-time.After(time.Millisecond)
	time.Sleep(500 * time.Millisecond)` + check + `}
`},
	}
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, level), func(t *testing.T) {
				t.Parallel()
				prog := mustBuild(t, single(c.src), optFlag(level))
				found := symbolsOf(t, prog, schedulerSymbols)
				if c.sched != (len(found) > 0) {
					t.Errorf("scheduler symbols in the binary: %v; the program starts the scheduler: %v", found, c.sched)
				}
				threads, out := sleepThreads(t, prog)
				if out != "sleeping\nelapsed ok true\n" {
					t.Errorf("output:\n%s", out)
				}
				if c.sched {
					if threads < 2 {
						t.Errorf("%d threads while sleeping, want the scheduler's monitor and more", threads)
					}
				} else if threads != 1 {
					t.Errorf("%d threads while sleeping, want 1", threads)
				}
			})
		}
	}
}
