package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// buildConc builds src as a main package with cfg's settings and returns
// the executable's path.
func buildConc(t *testing.T, src string, cfg build.Config) string {
	t.Helper()
	prog, err := buildIn(t.TempDir(), src, cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return prog
}

// buildIn builds src as a main package in dir with cfg's settings and
// returns the executable's path.
func buildIn(dir, src string, cfg build.Config) (string, error) {
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		return "", err
	}
	cfg.Dir = dir
	cfg.Output = filepath.Join(dir, "prog")
	cfg.Stdlib = plutonium.Stdlib()
	return cfg.Output, build.Build(&cfg)
}

// concBuild is a program, its name for messages, and the settings to build
// it with.
type concBuild struct {
	name, src string
	cfg       build.Config
}

// buildConcAll builds the programs at the same time, as many at once as
// there are processors, and returns the executables' paths in order. The
// tests that use it run the programs one at a time afterwards, since
// their timing should not compete with the compiler for the CPU.
func buildConcAll(t *testing.T, builds []concBuild) []string {
	t.Helper()
	progs := make([]string, len(builds))
	errs := make([]error, len(builds))
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, b := range builds {
		dir := t.TempDir()
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			progs[i], errs[i] = buildIn(dir, b.src, b.cfg)
		})
	}
	wg.Wait()
	failed := false
	for i, err := range errs {
		if err != nil {
			t.Errorf("%s: build: %v", builds[i].name, err)
			failed = true
		}
	}
	if failed {
		t.FailNow()
	}
	return progs
}

// runConc runs prog with the extra environment variables env and returns
// its output (stdout, then stderr) and exit status. A death by signal
// fails the test.
func runConc(t *testing.T, prog string, env ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(prog)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	status := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = exitErr.ExitCode()
		if status < 0 {
			t.Fatalf("program died: %v\noutput: %s%s", err, stdout.String(), stderr.String())
		}
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String() + stderr.String(), status
}

// concCase is a program checked at -O0 and -O2, and with one processor and
// with every CPU.
type concCase struct {
	name, src, want string
	status          int
}

func runConcCases(t *testing.T, cases []concCase, cfg build.Config) {
	progs := buildConcCases(t, cases, cfg)
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			prog := progs[0]
			progs = progs[1:]
			for _, procs := range []string{"1", ""} {
				name := c.name + "/O" + string(rune('0'+level))
				if procs != "" {
					name += "/procs" + procs
				}
				t.Run(name, func(t *testing.T) {
					var env []string
					if procs != "" {
						env = append(env, "PLUTO_MAXPROCS="+procs)
					}
					got, status := runConc(t, prog, env...)
					if status != c.status || got != c.want {
						t.Errorf("status %d, output:\n%s\nwant status %d, output:\n%s", status, got, c.status, c.want)
					}
				})
			}
		}
	}
}

// buildConcCases builds each case at -O0 and -O2 with cfg's other
// settings, with buildConcAll, and returns the executables in that order.
func buildConcCases(t *testing.T, cases []concCase, cfg build.Config) []string {
	t.Helper()
	var builds []concBuild
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			cfg := cfg
			cfg.OptLevel = level
			builds = append(builds, concBuild{name: c.name + "/O" + string(rune('0'+level)), src: c.src, cfg: cfg})
		}
	}
	return buildConcAll(t, builds)
}

// TestConcurrency runs programs that exercise the hosted scheduler: they
// only finish if preemption, work stealing, the poller and the handoff of
// blocked system calls work.
func TestConcurrency(t *testing.T) {
	runConcCases(t, []concCase{
		{name: "preempt", want: "preempted\n", src: `package main

import "pluto/sync/atomic"

// spin never waits in its loop: only preemption lets another puroutine
// run on its processor, and with one processor only that sets the flag.
// The flag is atomic, since two puroutines use it.
var flag atomic.Int32
var spins volatile[int64]

func spin(done chan bool) {
	for flag.Load() == 0 {
		spins = spins + 1
	}
	done <- true
}

func set() { flag.Store(1) }

func main() {
	done := make(chan bool)
	go spin(done)
	go set()
	<-done
	println("preempted")
}
`},
		{name: "stackleft", want: "main true\ngo true\nrt.Go true\n", src: `package main

import "pluto/rt"

// rt.StackLeft measures a puroutine's own stack, down to its guard page,
// and main's reserved //pu:stack stack, which a go statement in main
// leaves alone.
func left(done chan bool) {
	n := rt.StackLeft()
	lo, hi := rt.CurrentTask().Stack()
	done <- n > 0 && n < hi-lo
}

func big() {
	n := rt.StackLeft()
	lo, hi := rt.CurrentTask().Stack()
	println("rt.Go", n > 512<<10 && n < hi-lo)
	bigDone <- true
}

var bigDone = make(chan bool)

//pu:stack 16M
func main() {
	n := rt.StackLeft()
	println("main", n > 15<<20 && n <= 16<<20)
	done := make(chan bool)
	go left(done)
	println("go", <-done)
	rt.Go(big, 1<<20)
	<-bigDone
}
`},
		{name: "preemptmain", want: "main preempted\n", src: `package main

import "pluto/sync/atomic"

var flag atomic.Int32

func set() { flag.Store(1) }

// The main puroutine spins too; the one it started must still run.
func main() {
	go set()
	for flag.Load() == 0 {
	}
	println("main preempted")
}
`},
		{name: "blockingsyscall", want: "read 5 hello\n", src: `package main

import (
	"pluto/rt"
	"pluto/syscall"
	"unsafe"
)

// A puroutine blocks in read on a pipe in blocking mode, announced with
// EnterSyscall. With one processor the writer can only run if the
// scheduler hands the processor to another worker while read blocks.
var fds [2]int32

func reader(done chan string) {
	var buf [16]byte
	rt.EnterSyscall()
	n, _ := syscall.Read(int(fds[0]), buf[:])
	rt.ExitSyscall()
	println("read", n, unsafe.String(&buf[0], n))
	done <- "ok"
}

func writer() {
	x := 0
	for i := 0; i < 1000000; i++ {
		x += i
	}
	msg := "hello"
	syscall.Write(int(fds[1]), unsafe.Slice(unsafe.StringData(msg), len(msg)))
}

func main() {
	syscall.Syscall(syscall.SYS_PIPE2, uintptr(unsafe.Pointer(&fds[0])), 0, 0)
	done := make(chan string)
	go reader(done)
	go writer()
	<-done
}
`},
		{name: "poller", want: "got 40000 bytes\n", src: `package main

import (
	"pluto/alloc"
	"pluto/os"
	"pluto/rt"
)

// os.Pipe's descriptors are non-blocking: the reader of an empty pipe parks
// in the poller, so the writer runs even with one processor, and the
// writer parks there too when the pipe is full.
func writer(w *os.File) {
	var chunk [1000]byte
	for i := range chunk {
		chunk[i] = 'x'
	}
	for i := 0; i < 40; i++ {
		w.Write(chunk[:])
		rt.Yield()
	}
	w.Close()
}

func main() {
	r, w, err := os.Pipe()
	if err != nil {
		println("pipe:", err.Error())
		return
	}
	go writer(w)
	var buf [256]byte
	total := 0
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			break
		}
		total += n
	}
	r.Close()
	alloc.Free(r)
	alloc.Free(w)
	println("got", total, "bytes")
}
`},
		{name: "execpipe", want: "run true\nread 200000\n", src: `package main

import (
	"pluto/alloc"
	"pluto/os"
	"pluto/os/exec"
)

// A pipe from os.Pipe handed to a child as its standard output: exec takes
// its descriptor with Fd, which makes it blocking again, so the child
// waits when the pipe is full instead of failing with EAGAIN. Meanwhile a
// puroutine drains the read end through the poller while main waits for
// the child.
func drain(r *os.File, done chan int) {
	var buf [4096]byte
	total := 0
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			break
		}
		total += n
	}
	done <- total
}

func main() {
	r, w, err := os.Pipe()
	if err != nil {
		println("pipe:", err.Error())
		return
	}
	done := make(chan int)
	go drain(r, done)
	c := exec.Command("sh", "-c", "head -c 200000 /dev/zero")
	c.Stdout = w
	err = c.Run()
	println("run", err == nil)
	c.Drop()
	w.Close()
	println("read", <-done)
	r.Close()
	alloc.Free(r)
	alloc.Free(w)
}
`},
		{name: "fairness", want: "fair true\n", src: `package main

// Two always-ready cases: select picks each about half the time.
func main() {
	a := make(chan int, 1)
	b := make(chan int, 1)
	na, nb := 0, 0
	for i := 0; i < 20000; i++ {
		a <- 1
		b <- 1
		select {
		case <-a:
			na++
			<-b
		case <-b:
			nb++
			<-a
		}
	}
	println("fair", na > 8000 && nb > 8000)
}
`},
		{name: "deadlock", status: 2, want: "fatal error: all puroutines are asleep - deadlock!\n\npuroutine 1 [chan receive]:\n", src: `package main

func main() {
	c := make(chan int)
	<-c
}
`},
		{name: "pingpong", want: "pingpong 200000\n", src: `package main

func pong(in, out chan int) {
	for v := range in {
		out <- v + 1
	}
	close(out)
}

func main() {
	a := make(chan int)
	b := make(chan int)
	go pong(a, b)
	v := 0
	for i := 0; i < 200000; i++ {
		a <- v
		v = <-b
	}
	close(a)
	if _, ok := <-b; ok {
		println("b not closed")
	}
	println("pingpong", v)
}
`},
		{name: "nilclosure", status: 2, want: "panic: go of a function value that captures variables or binds a method value: its closure record lives in the starting frame; pass the values as arguments\n", src: `package main

import "pluto/rt"

func run(f *func()) {
	go (*f)()
}

// The escape analysis rejects passing the literal to a function that
// starts it (spec section 18.3); rt.Pinned hides it, so the check at run
// time is what stops it.
func main() {
	n := 1
	f := func() { n++ }
	run(rt.Pinned(&f))
}
`},
		{name: "waitgroupgoclosure", status: 2, want: "panic: go of a function value that captures variables or binds a method value: its closure record lives in the starting frame; pass the values as arguments\n", src: `package main

import (
	"pluto/rt"
	"pluto/sync"
)

// The compiler rejects a capturing literal passed to wg.Go, or to a
// function that passes it on to wg.Go, where it can see the call
// (TestGoFuncCall), and the escape analysis rejects it anywhere else;
// rt.Pinned hides it from both, so the check when Go runs catches it.
func start(wg *sync.WaitGroup, f func()) {
	wg.Go(f)
}

func main() {
	var wg sync.WaitGroup
	n := 1
	f := func() { n++ }
	start(&wg, *rt.Pinned(&f))
	wg.Wait()
}
`},
		{name: "allocuse", want: "while used 1\nafter 1 restored true\n", src: `package main

import (
	"pluto/alloc"
	"unsafe"
)

// counting counts the allocations made from it.
type counting struct {
	next alloc.Allocator
	n    int
}

func (c *counting) Alloc(size, align uintptr) unsafe.Pointer {
	c.n++
	return c.next.Alloc(size, align)
}

func (c *counting) Free(p unsafe.Pointer, size, align uintptr) { c.next.Free(p, size, align) }

var mine counting

// other allocates with the program-wide default: alloc.Use in main does
// not reach it.
func other(done chan int) {
	p := new(int)
	*p = 5
	done <- *p
}

func main() {
	mine.next = alloc.Default()
	r := alloc.Use(&mine)
	done := make(chan int) // from mine: 1
	go other(done)         // its stack is reserved from the system, not an allocator
	<-done
	println("while used", mine.n)
	r.Restore()
	_ = new(int)
	println("after", mine.n, "restored", alloc.Default() == mine.next)
}
`},
		{name: "interruptcontext", status: 2, want: "non-blocking send ok\nreceived 1\npanic: blocking operation in interrupt context\n", src: `package main

import "pluto/rt"

// A backend that says the processor is handling an interrupt: operations
// that complete at once still work, and one that would park panics.
type irqSched struct{}

//pu:scheduler
var sched irqSched

var inIRQ bool

func (irqSched) NewTask(req rt.StackRequest) *rt.Task { return nil }
func (irqSched) FreeTask(t *rt.Task)                  {}
func (irqSched) Spawn(t *rt.Task)                     {}
func (irqSched) Park(t *rt.Task)                      {}
func (irqSched) Wake(t *rt.Task)                      {}
func (irqSched) Yield(t *rt.Task)                     {}
func (irqSched) InInterrupt() bool                    { return inIRQ }

func main() {
	c := make(chan int, 1)
	inIRQ = true
	select {
	case c <- 1:
		println("non-blocking send ok")
	default:
	}
	println("received", <-c)
	<-c
	println("unreachable")
}
`},
	}, build.Config{NoTrace: true})
}

// TestWorkStealing starts many CPU-bound puroutines and checks that they
// ran on more than one worker thread.
func TestWorkStealing(t *testing.T) {
	if runtime.NumCPU() < 2 {
		t.Skip("one CPU")
	}
	prog := buildConc(t, `package main

import (
	"pluto/sync"
	"pluto/sync/atomic"
	"pluto/syscall"
)

var (
	wg      sync.WaitGroup
	mu      sync.Mutex
	tids    [1024]int
	ntids   int
	sum     atomic.Int64
)

func work(n int) {
	x := 0
	for i := 0; i < 20000000; i++ {
		x += i ^ n
	}
	sum.Add(int64(x & 1))
	tid := int(syscall.Syscall(186, 0, 0, 0)) // gettid
	mu.Lock()
	seen := false
	for i := 0; i < ntids; i++ {
		seen = seen || tids[i] == tid
	}
	if !seen && ntids < len(tids) {
		tids[ntids] = tid
		ntids++
	}
	mu.Unlock()
	wg.Done()
}

func main() {
	const n = 64
	wg.Add(n)
	for i := 0; i < n; i++ {
		go work(i)
	}
	wg.Wait()
	mu.Lock()
	println("threads", ntids)
	mu.Unlock()
}
`, build.Config{OptLevel: 2})
	out, status := runConc(t, prog)
	if status != 0 || !strings.HasPrefix(out, "threads ") || out == "threads 1\n" {
		t.Errorf("status %d, output %q: want the work spread over several threads", status, out)
	}
}

// parallelAllocProgram allocates and frees from many puroutines at once and
// checks that no two live blocks overlap: each block is filled with its
// owner's pattern and checked before it is freed.
const parallelAllocProgram = `package main

import (
	"pluto/alloc"
	"pluto/sync"
	"pluto/sync/atomic"
	"unsafe"
)

var wg sync.WaitGroup
var bad atomic.Int64 // every puroutine counts here

func churn(id int) {
	var live [32]unsafe.Pointer
	var sizes [32]uintptr
	seed := uint64(id)*0x9E3779B97F4A7C15 + 1
	for i := 0; i < 20000; i++ {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		k := int(seed % 32)
		if p := live[k]; p != nil {
			b := unsafe.Slice((*byte)(p), sizes[k])
			for j := range b {
				if b[j] != byte(id) {
					bad.Add(1)
					break
				}
			}
			alloc.Default().Free(p, sizes[k], 8)
			live[k] = nil
			continue
		}
		size := uintptr(seed>>8)%3000 + 1
		p := alloc.Raw(size, 8)
		b := unsafe.Slice((*byte)(p), size)
		for j := range b {
			if b[j] != 0 {
				bad.Add(1)
				break
			}
			b[j] = byte(id)
		}
		live[k] = p
		sizes[k] = size
	}
	wg.Done()
}

func main() {
	const n = 48
	wg.Add(n)
	for i := 0; i < n; i++ {
		go churn(i)
	}
	wg.Wait()
	println("corrupt blocks", bad.Load())
}
`

// TestParallelAlloc checks the default allocator, and the debug
// allocator, with allocations and frees from every worker at once.
func TestParallelAlloc(t *testing.T) {
	testguard.Parallel(t) // it checks the heap, not timing
	cfgs := []build.Config{{OptLevel: 2}, {OptLevel: 2, Alloc: "debug"}}
	progs := buildConcAll(t, []concBuild{
		{name: "default allocator", src: parallelAllocProgram, cfg: cfgs[0]},
		{name: "debug allocator", src: parallelAllocProgram, cfg: cfgs[1]},
	})
	for i, cfg := range cfgs {
		out, status := runConc(t, progs[i], "PLUTO_DEBUGALLOC=leaks=off")
		if status != 0 || out != "corrupt blocks 0\n" {
			t.Errorf("alloc %q: status %d, output:\n%s", cfg.Alloc, status, out)
		}
	}
}

// TestConcDebugAlloc runs concurrent programs that free everything they
// allocate, channels included, under the debug allocator with leaks
// failing the program.
func TestConcDebugAlloc(t *testing.T) {
	prog := buildConc(t, `package main

import "pluto/sync"

var wg sync.WaitGroup

func worker(in <-chan int, out chan<- int) {
	for v := range in {
		out <- v * v
	}
	wg.Done()
}

func main() {
	in := make(chan int)
	out := make(chan int, 100)
	wg.Add(4)
	for i := 0; i < 4; i++ {
		go worker(in, out)
	}
	for i := 0; i < 100; i++ {
		in <- i
	}
	close(in)
	wg.Wait()
	sum := 0
	for i := 0; i < 100; i++ {
		sum += <-out
	}
	println("sum", sum)
}
`, build.Config{OptLevel: 2, Alloc: "debug"})
	out, status := runConc(t, prog, "PLUTO_DEBUGALLOC=leaks=fail")
	if status != 0 || out != "sum 328350\n" {
		t.Errorf("status %d, output:\n%s", status, out)
	}
}
