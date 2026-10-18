package build_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Section 15.4, When main returns: the program ends as in Go. Tasks that are
// still running are not waited for; package cleanup runs only when none was.
func TestNormalTerminationAbandonsTasks(t *testing.T) {
	testguard.Parallel(t)
	for _, tc := range []struct{ name, source string }{
		{"spinning", `package main
func spin() { for { } }
func main() int64 { task spin(); println("main"); return 7 }
`},
		{"blocked", `package main
func block(c chan int) { <-c }
func main() int64 { c := make(chan int); task block(c); println("main"); return 7 }
`},
		{"sleeping", `package main
import "pluto/time"
func nap() { time.Sleep(time.Hour) }
func main() int64 { task nap(); println("main"); return 7 }
`},
		{"descendant", `package main
func spin() { for { } }
func parent() { task spin() }
func main() int64 { task parent(); println("main"); return 7 }
`},
	} {
		// The program ends with main's status and its output, without the
		// cleanup of the package variable that follows.
		source := tc.source + `
type resource owner struct{ live bool }
func (r *resource) Drop() { if r.live { println("package cleanup"); r.live = false } }
var global = resource{live: true}
`
		for _, opt := range []int{0, 2} {
			t.Run(tc.name+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runNormalShutdown(t, source, opt)
				// A task that runs before main prints nothing, so the
				// output is exactly main's.
				if status != 7 || out != "main\n" {
					t.Fatalf("status %d, output %q; want status 7, \"main\\n\"", status, out)
				}
			})
		}
	}
}

// The tasks of a joined program have finished, so package cleanup runs. A
// sync.Scope join guarantees it; a sync.WaitGroup or channel join makes it
// likely (decision D31), and the tasks here do nothing after the wake-up that
// could delay their end beyond the runtime's short wait.
func TestNormalTerminationJoinedTasks(t *testing.T) {
	testguard.Parallel(t)
	const resource = `
type resource owner struct{ live bool }
func (r *resource) Drop() { if r.live { println("package cleanup"); r.live = false } }
var global = resource{live: true}
`
	for _, tc := range []struct{ name, body string }{
		{"scope", `package main
import "pluto/sync"
func work(n *int) { *n = *n + 1 }
func main() int64 {
	var s sync.Scope
	var a, b int
	s.Task(func() { work(&a) })
	s.Task(func() { work(&b) })
	s.Wait()
	println("main", a+b)
	return 3
}`},
		{"waitgroup", `package main
import ("pluto/sync"; "pluto/sync/atomic")
var wg sync.WaitGroup
var count atomic.Int32
func bump() { count.Add(1) }
func main() int64 {
	wg.Task(bump)
	wg.Wait()
	println("main", count.Load())
	return 3
}`},
		{"channel", `package main
func send(c chan int) { c <- 1 }
func main() int64 {
	c := make(chan int)
	task send(c)
	println("main", <-c)
	return 3
}`},
	} {
		for _, opt := range []int{0, 2} {
			t.Run(tc.name+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				testguard.Parallel(t)
				for i := 0; i < 5; i++ {
					out, status := runNormalShutdown(t, tc.body+resource, opt)
					if status != 3 || !strings.HasSuffix(out, "\npackage cleanup\n") {
						t.Fatalf("run %d: status %d, output %q; want status 3 and package cleanup", i, status, out)
					}
				}
			})
		}
	}
}

// The debug allocator names the tasks that were still running and skips its
// leak report; a program whose tasks have finished gets the report.
func TestNormalTerminationDebugAllocator(t *testing.T) {
	testguard.Parallel(t)
	const abandoned = `package main
import "pluto/alloc"
func hold(c chan int) { p := alloc.New[[16]byte](); _ = p; <-c }
func main() int64 {
	c := make(chan int)
	task hold(c)
	task hold(c)
	q := alloc.New[[8]byte]() // a leak, but live tasks make the report meaningless
	_ = q
	return 5
}
`
	const joined = `package main
import "pluto/alloc"
func main() {
	q := alloc.New[[8]byte]()
	_ = q
}
`
	for _, opt := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(opt), func(t *testing.T) {
			testguard.Parallel(t)
			prog := buildDebugAlloc(t, abandoned, opt)
			out, status := runDebugAlloc(t, prog, "")
			want := "debug allocator: 2 tasks still running at exit; the leak report is skipped\n"
			if status != 5 || out != want {
				t.Fatalf("status %d, output %q; want status 5, %q", status, out, want)
			}
			prog = buildDebugAlloc(t, joined, opt)
			out, status = runDebugAlloc(t, prog, "")
			if status != 2 || !strings.Contains(out, "1 allocation still live") || strings.Contains(out, "running") {
				t.Fatalf("status %d, output %q; want status 2 and a leak report", status, out)
			}
		})
	}
}

// An armed time.AfterFunc timer is not a running task: main returns, the
// timer is canceled as Stop cancels it, and the timer is released, so the
// debug allocator reports nothing and the program exits with main's status.
func TestNormalTerminationPendingTimer(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/time"
func later() { println("unexpected") }
func main() int64 {
	t := time.AfterFunc(time.Hour, later)
	_ = t
	t2 := time.AfterFunc(time.Hour, later)
	_ = t2
	return 4
}
`
	for _, opt := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(opt), func(t *testing.T) {
			testguard.Parallel(t)
			prog := buildDebugAlloc(t, source, opt)
			out, status := runDebugAlloc(t, prog, "")
			if status != 4 || out != "" {
				t.Fatalf("status %d, output %q; want status 4 and no output", status, out)
			}
		})
	}
}
