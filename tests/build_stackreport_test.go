package tests

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The stack bounds of task statements (language spec section 19.1, item 1;
// roadmap item 18): a task whose call graph is bounded starts with the stack
// the compiler proved it needs, and --stackreport says what each task
// statement got.

// stackKnownFail says why the tests that expect a proven stack fail for
// now: the runtime's own call graph, which every task reaches, has cycles
// (the panic path prints with bounds checks that panic, the scheduler's
// unlocks may yield), so no task's stack is proven yet. Roadmap item 18,
// step A, breaks the cycles for builds with --gencheck=false; then this is
// "" again. As with a corpus case's knownfail note, a test that fails while
// it is set is skipped, and one that passes is an error.
const stackKnownFail = "roadmap item 18 step A: the runtime's call graph has cycles, so no task stack is proven yet"

// knownFail reports fails, the failures of a test, under note: skips the
// test if it is set and the test failed, and fails it if it is set and the
// test passed.
func knownFail(t *testing.T, note string, fails []string) {
	t.Helper()
	switch {
	case note != "" && len(fails) > 0:
		t.Skipf("known failure: %s (%d failures, the first: %s)", note, len(fails), fails[0])
	case note != "":
		t.Errorf("passes but is marked as a known failure (%s): clear stackKnownFail", note)
	}
	for _, f := range fails {
		t.Error(f)
	}
}

// stackReportLine matches a line of --stackreport.
var stackReportLine = regexp.MustCompile(`^(\S*main\.p:\d+:\d+): task stack: (?:(\d+) bytes, proven|not computed: (.*))$`)

// stackReport builds files with --stackreport and flags and returns the
// lines the report gave for main.p, in order, as "proven N" or the reason,
// and the executable.
func stackReport(t *testing.T, files map[string]string, flags ...string) ([]string, []int64, string) {
	t.Helper()
	r := tryBuild(t, files, append([]string{"--stackreport"}, flags...)...)
	if r.status != 0 {
		t.Fatalf("build %s: exit status %d\n%s", strings.Join(flags, " "), r.status, r.out)
	}
	var lines []string
	var bounds []int64
	for line := range strings.SplitSeq(r.out, "\n") {
		m := stackReportLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[2] != "" {
			n, _ := strconv.ParseInt(m[2], 10, 64)
			lines = append(lines, "proven")
			bounds = append(bounds, n)
		} else {
			lines = append(lines, m[3])
			bounds = append(bounds, 0)
		}
	}
	return lines, bounds, r.exe
}

// TestStackReport checks which task statements get a proven stack and why
// the others do not: recursion, a call through a func value that may reach a
// recursive function, and a function whose body the compiler cannot see.
func TestStackReport(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"main.p": `package main

//pu:noinline
func leaf(n int) int {
	var b [64]int
	b[n&63] = n
	return b[(n+1)&63]
}

func bounded(done chan<- int) { done <- leaf(3) }

//pu:noinline
func rec(n int) int {
	if n == 0 {
		return 0
	}
	return rec(n-1) + 1
}

func recursive(done chan<- int) { done <- rec(10) }

var fs = []func(int) int{rec, leaf}

func indirect(done chan<- int) { done <- fs[len(fs)-2](3) }

//pu:extern stk_pushes
func pushes() int

func unknown(done chan<- int) { done <- pushes() }

//pu:extern stk_leaf
func asmLeaf() int

func asmCaller(done chan<- int) { done <- asmLeaf() }

func main() {
	done := make(chan int)
	task bounded(done)
	println(<-done)
	task recursive(done)
	println(<-done)
	task indirect(done)
	println(<-done)
	task unknown(done)
	println(<-done)
	task asmCaller(done)
	println(<-done)
}
`,
		"stubs_amd64.s": `	.text
	.globl	stk_pushes
stk_pushes:
	pushq	%rbx
	movq	$7, %rax
	popq	%rbx
	ret
	.globl	stk_leaf
stk_leaf:
	movq	$5, %rax
	ret
	.section .note.GNU-stack,"",@progbits
`,
	}
	var fails []string
	for _, level := range []int{0, 2} {
		lines, bounds, exe := stackReport(t, files, optFlag(level), "--gencheck=false")
		want := []string{
			"proven",
			"main.rec is recursive: main.rec calls main.rec",
			"main.rec is recursive: main.rec calls main.rec",
			"stk_pushes has no body whose stack use is known",
			"proven",
		}
		match := len(lines) == len(want)
		for i := 0; match && i < len(want); i++ {
			match = strings.HasPrefix(lines[i], want[i])
		}
		if !match {
			fails = append(fails, fmt.Sprintf("-O%d: report:\n%s\nwant:\n%s", level, strings.Join(lines, "\n"), strings.Join(want, "\n")))
			continue
		}
		for _, i := range []int{0, 4} {
			if bounds[i] < 8192 || bounds[i]%16 != 0 {
				fails = append(fails, fmt.Sprintf("-O%d: bound %d", level, bounds[i]))
			}
		}
		if out, status := runExeEnv(t, exe); status != 0 || out != "0\n10\n3\n7\n5\n" {
			fails = append(fails, fmt.Sprintf("-O%d: status %d, output:\n%s", level, status, out))
		}
	}
	knownFail(t, stackKnownFail, fails)
}

// TestStackReportGenCheck checks that a build with generation checks
// proves no task stack, and says why: the checks walk a record's chain of
// parents recursively (genLive), as deep as allocators nest.
func TestStackReportGenCheck(t *testing.T) {
	t.Parallel()
	const src = `package main

func run(done chan<- int) { done <- 1 }

func main() {
	done := make(chan int)
	task run(done)
	println(<-done)
}
`
	for _, level := range []int{0, 2} {
		lines, _, exe := stackReport(t, single(src), optFlag(level), "--gencheck=true")
		if len(lines) != 1 || !strings.Contains(lines[0], " is recursive: ") {
			t.Errorf("-O%d: report %q, want one task not computed for recursion", level, lines)
		}
		if out, status := runExeEnv(t, exe); status != 0 || out != "1\n" {
			t.Errorf("-O%d: status %d, output:\n%s", level, status, out)
		}
	}
}

// stackHighWater is a program whose task fills its stack with a pattern,
// runs a workload of large frames, deferred calls and calls through an
// interface whose callees the compiler knows, and prints how much of its
// stack the workload touched and how large its stack is.
const stackHighWater = `package main

import (
	"pluto/rt"
	"unsafe"
)

const pattern = 0xA5

// fill sets the stack below its caller's frame, but for 256 bytes of room
// for itself, to the pattern.
//
//pu:noinline
func fill(lo uintptr) {
	var here byte
	sp := uintptr(unsafe.Pointer(&here)) - 256
	for p := lo; p < sp; p++ {
		*(*byte)(unsafe.Pointer(p)) = pattern
	}
}

type shape interface{ area(n int) int }

type square struct{ k int }

//pu:noinline
func (s square) area(n int) int {
	var b [512]int
	for i := range b {
		b[i] = i * s.k
	}
	return b[n&511]
}

type disc struct{ k int }

//pu:noinline
func (d disc) area(n int) int {
	var b [1536]int
	for i := range b {
		b[i] = i + d.k
	}
	return deep(b[n&1535])
}

//pu:noinline
func deep(n int) int {
	var b [2048]byte
	b[n&2047] = byte(n)
	return int(b[(n+1)&2047]) + n
}

var total int

//pu:noinline
func note(n int) { total += n }

//pu:noinline
func work(s shape, n int) int {
	defer note(n)
	var pad [300]int
	pad[n%300] = s.area(n)
	return pad[n%300]
}

func run(done chan<- [3]int) {
	lo, hi := rt.CurrentTask().Stack()
	fill(lo)
	shapes := [3]shape{square{2}, disc{3}, square{4}}
	sum := 0
	for i, s := range shapes {
		sum += work(s, i+1)
	}
	p := lo
	for p < hi && *(*byte)(unsafe.Pointer(p)) == pattern {
		p++
	}
	done <- [3]int{int(hi - p), int(hi - lo), sum + total}
}

func main() {
	done := make(chan [3]int)
	task run(done)
	r := <-done
	println("used", r[0], "size", r[1], "sum", r[2])
}
`

// TestStackHighWater checks that a task with a proven stack runs within
// it at -O0 and -O2, built with --gencheck=false (TestStackReportGenCheck
// covers checked builds), and that the
// stack the task touched, found from a pattern its stack was filled with,
// is no more than the bound the compiler computed, and that the bound is
// not far more than it needs.
func TestStackHighWater(t *testing.T) {
	t.Parallel()
	used := regexp.MustCompile(`^used (\d+) size (\d+) sum (\d+)\n$`)
	var fails []string
	for _, level := range []int{0, 2} {
		for _, gencheck := range []string{"--gencheck=false"} {
			lines, bounds, exe := stackReport(t, single(stackHighWater), optFlag(level), gencheck)
			if len(lines) != 1 || lines[0] != "proven" {
				fails = append(fails, fmt.Sprintf("-O%d %s: report %q", level, gencheck, lines))
				continue
			}
			for _, procs := range taskStackProcs {
				out, status := runExeEnv(t, exe, "PLUTO_MAXPROCS="+procs)
				m := used.FindStringSubmatch(out)
				if status != 0 || m == nil {
					fails = append(fails, fmt.Sprintf("-O%d %s, %s processors: status %d, output:\n%s", level, gencheck, procs, status, out))
					continue
				}
				u, _ := strconv.ParseInt(m[1], 10, 64)
				size, _ := strconv.ParseInt(m[2], 10, 64)
				if u > bounds[0] || size < bounds[0] || u < 12<<10 {
					fails = append(fails, fmt.Sprintf("-O%d %s, %s processors: bound %d, stack %d, used %d", level, gencheck, procs, bounds[0], size, u))
				}
				if testing.Verbose() {
					t.Logf("-O%d %s, %s processors: bound %d, stack %d, used %d", level, gencheck, procs, bounds[0], size, u)
				}
			}
		}
	}
	knownFail(t, stackKnownFail, fails)
}

// TestStackBoundPanics checks that a task with a proven stack that panics,
// or faults on a nil pointer, runs its deferred calls and reports the
// panic, which runs the panic path on the task's own stack.
func TestStackBoundPanics(t *testing.T) {
	t.Parallel()
	var fails []string
	for _, tc := range []struct{ name, src, want string }{
		{"panic", panicTask("panic(\"boom\")"), "panic: boom"},
		{"nil", panicTask("println(get(&node{}))"), "panic: runtime error: invalid memory address or nil pointer dereference"},
	} {
		for _, level := range []int{0, 2} {
			lines, _, exe := stackReport(t, single(tc.src), optFlag(level), "--gencheck=false")
			if len(lines) != 1 || lines[0] != "proven" {
				fails = append(fails, fmt.Sprintf("%s -O%d: report %q", tc.name, level, lines))
				continue
			}
			out, status := runExeEnv(t, exe)
			if status != 2 || !strings.HasPrefix(out, "deferred two\ndeferred one\n"+tc.want) || strings.Contains(out, "stack overflow") {
				fails = append(fails, fmt.Sprintf("%s -O%d: status %d, output:\n%s", tc.name, level, status, out))
			}
		}
	}
	knownFail(t, stackKnownFail, fails)
}

// panicTask is a program whose one task runs two deferred calls and then
// does stmt.
func panicTask(stmt string) string {
	return `package main

//pu:noinline
func cleanup(s string) { println("deferred", s) }

type node struct {
	next *node
	v    int
}

//pu:noinline
func get(n *node) int { return n.next.v }

func explode() {
	defer cleanup("one")
	defer cleanup("two")
	` + stmt + `
}

func main() {
	task explode()
	<-make(chan int)
}
`
}
