package build_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// The stacks of hosted tasks whose size the compiler cannot compute
// (language spec section 19.1, complete-spec gap G36): 64 MiB of address
// space each, committed as it is touched, with a guard region below.

var taskProcs = []string{"1", "2", "24"}

// TestTaskDeepStack checks that a task can recurse 100,000 calls deep and
// keep a 128 KiB local array, which overflowed the 64 KiB stacks tasks had
// before, and that its stack has the room HostedStackSize promises.
func TestTaskDeepStack(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/rt"

//pu:noinline
func depth(n int) int {
	var pad [4]int
	pad[n&3] = n
	if n == 0 {
		return 0
	}
	return depth(n-1) + 1 + pad[(n+1)&3]&0
}

//pu:noinline
func big() int {
	var buf [128 << 10]byte
	for i := range buf {
		buf[i] = byte(i)
	}
	return int(buf[len(buf)-1])
}

func run(n int, done chan<- int) { done <- depth(n) }

func room(done chan<- bool) { done <- rt.StackLeft() > rt.HostedStackSize-(64<<10) }

func large(done chan<- int) { done <- big() }

func main() {
	done := make(chan int)
	task run(100000, done)
	println("depth", <-done)
	ok := make(chan bool)
	task room(ok)
	println("room", <-ok)
	task large(done)
	println("large", <-done)
}
`
	for _, level := range []int{0, 2} {
		prog := buildConc(t, src, build.Config{OptLevel: level})
		for _, procs := range taskProcs {
			out, status := runConc(t, prog, "PLUTO_MAXPROCS="+procs)
			if want := "depth 100000\nroom true\nlarge 255\n"; status != 0 || out != want {
				t.Errorf("-O%d, %s processors: status %d, output:\n%s\nwant:\n%s", level, procs, status, out, want)
			}
		}
	}
}

// TestTaskStackOverflow checks that a task that runs off the end of its
// reserved stack lands in the guard region and is reported as a stack
// overflow with its trace, as on a stack from the allocator.
func TestTaskStackOverflow(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

func recurse(n int) int {
	var buf [64]int
	buf[n&63] = n
	return recurse(n+1) + buf[(n+1)&63]
}

func run(done chan bool) {
	println(recurse(0))
	done <- true
}

func main() {
	done := make(chan bool)
	task run(done)
	<-done
}
`
	for _, level := range []int{0, 2} {
		prog := buildConc(t, src, build.Config{OptLevel: level})
		for _, procs := range taskProcs {
			out, status := runConc(t, prog, "PLUTO_MAXPROCS="+procs)
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			frame := "    main.recurse (./main.p:"
			ok := status == 2 && len(lines) == 2+1+64+1 &&
				lines[0] == "panic: runtime error: stack overflow" &&
				strings.HasPrefix(lines[1], "[signal SIGSEGV") &&
				lines[2] == "task 2:" &&
				strings.HasPrefix(lines[3], frame) &&
				lines[len(lines)-1] == "    ...additional frames elided..."
			if !ok {
				t.Errorf("-O%d, %s processors: status %d, output:\n%s", level, procs, status, out)
			}
		}
	}
}

// manyTasks starts 10,000 tasks that sleep at once and reports how much
// the resident set grew, and how cheap starting and ending tasks is.
const manyTasks = `package main

import (
	"pluto/sync"
	"pluto/syscall"
	"pluto/time"
	"unsafe"
)

// maxRSS returns the largest resident set the process has had, in KiB
// (getrusage's ru_maxrss).
func maxRSS() int64 {
	var ru [18]int64
	syscall.Syscall(98, 0, uintptr(unsafe.Pointer(&ru)), 0)
	return ru[4]
}

var wg sync.WaitGroup

func sleeper() {
	time.Sleep(200 * time.Millisecond)
	wg.Done()
}

func main() {
	base := maxRSS()
	const n = 10000
	wg.Add(n)
	for i := 0; i < n; i++ {
		task sleeper()
	}
	wg.Wait()
	growth := maxRSS() - base
	// A sleeping task touches the page that holds its record and the top
	// of its stack, and little more.
	if growth > 16*n {
		println("resident set grew by", growth, "KiB")
	}
	println("slept", n)
}
`

// TestManyTasks checks that 10,000 tasks that sleep at once fit, each with
// a reserved stack, and that the resident set stays small: a task commits
// only the stack it touches. With an address-space limit (ulimit -v), the
// stacks get smaller as they use it up, so the tasks still fit.
func TestManyTasks(t *testing.T) {
	testguard.Parallel(t)
	prog := buildConc(t, manyTasks, build.Config{OptLevel: 2})
	for _, procs := range taskProcs {
		out, status := runConc(t, prog, "PLUTO_MAXPROCS="+procs)
		if status != 0 || out != "slept 10000\n" {
			t.Errorf("%s processors: status %d, output:\n%s", procs, status, out)
		}
	}
	// 2 GB of address space: about 30 stacks at full size would fill it.
	cmd := exec.Command("sh", "-c", `ulimit -v 2000000 && exec "$0"`, prog)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil || out.String() != "slept 10000\n" {
		t.Errorf("with ulimit -v 2000000: %v, output:\n%s", err, out.String())
	}
}

// TestTaskStackNotInArena checks that a task started while the starting
// task has a local arena installed with alloc.Use gets its stack from the
// program-wide default allocator, never from the arena, which may be reset
// while the new task still runs (language spec section 19.1).
func TestTaskStackNotInArena(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/rt"
	"unsafe"
)

var buf [1 << 20]byte

var done chan uintptr

// run reports where its stack is.
func run() {
	var x int
	done <- uintptr(unsafe.Pointer(&x))
}

func main() {
	done = make(chan uintptr) // before the arena is installed
	a := arena.New(buf[:])
	r := alloc.Use(&a)
	rt.Go(run, 64<<10)
	used := a.Used()
	r.Restore()
	p := <-done
	lo := uintptr(unsafe.Pointer(&buf[0]))
	println("arena used", used, "stack in arena", p >= lo && p < lo+uintptr(len(buf)))
}
`
	for _, level := range []int{0, 2} {
		prog := buildConc(t, src, build.Config{OptLevel: level})
		for _, procs := range taskProcs {
			out, status := runConc(t, prog, "PLUTO_MAXPROCS="+procs)
			if want := "arena used 0 stack in arena false\n"; status != 0 || out != want {
				t.Errorf("-O%d, %s processors: status %d, output:\n%s\nwant:\n%s", level, procs, status, out, want)
			}
		}
	}
}
