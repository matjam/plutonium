//pu:build linux

// Package testing supports tests and benchmarks run by pluto test.
//
// A test is a function in a _test.p file of the package it tests:
//
//	func TestXxx(t *testing.T)
//
// where Xxx does not start with a lower-case letter. A benchmark is
//
//	func BenchmarkXxx(b *testing.B)
//
// and runs b.N iterations of the code it measures. pluto test generates a
// main package that runs them through M and prints the results in the
// format of go test.
//
// Nothing here allocates. Messages are formatted with pluto/fmt into
// fixed buffers on the stack (longer messages are cut short), and names
// of subtests are built in a fixed buffer in each T.
//
// FailNow, Fatal, Fatalf, SkipNow, Skip and Skipf end the test function
// with rt.Goexit, as Go's do with runtime.Goexit: the deferred calls of the
// test function and of every function it was in run, and their variables
// are released, before the next test starts. The runner stops the exit
// with rt.CatchGoexit, so tests run one after another on the task that
// runs main. A test that panics ends the whole test binary, after running
// its deferred calls; pluto test reports it as a failure of that test.
package testing

import (
	"pluto/fmt"
	"pluto/os"
	"pluto/rt"
	"pluto/syscall"
	"pluto/vec"
	"unsafe"
)

const (
	maxName   = 256 // longest test name, with its parents', kept
	maxLevels = 8   // deepest subtest level a -run pattern can select
	msgSize   = 1024
	benchTime = 1000000000 // nanoseconds a benchmark should run for
	maxN      = 1000000000
)

// M runs the tests and benchmarks of one package. pluto test generates
// the main function that drives it.
type M struct {
	pkg       string
	chatty    bool
	nameWidth int
	filters   [maxLevels]string
	nfilters  int
	ran       bool
	failed    bool
	header    bool
}

// Init prepares m for the package with the given import path. With
// chatty set, the binary runs in verbose mode. nameWidth is the length of
// the longest benchmark name, to which benchmark results are aligned.
func (m *M) Init(pkg string, chatty bool, nameWidth int) {
	m.pkg = pkg
	m.chatty = chatty
	m.nameWidth = nameWidth
}

// Filter adds the pattern for the next level of subtests. A pattern is
// literal text that must occur in the subtest's name, anchored to its
// start by a leading ^ and to its end by a trailing $.
func (m *M) Filter(pattern string) {
	if m.nfilters < maxLevels {
		m.filters[m.nfilters] = pattern
		m.nfilters++
	}
}

// matches reports whether a subtest at the given depth (1 for a subtest of
// a top-level test) with the given name element is selected.
func (m *M) matches(depth int, elem string) bool {
	if depth > m.nfilters {
		return true
	}
	p := m.filters[depth-1]
	start := len(p) > 0 && p[0] == '^'
	if start {
		p = p[1:]
	}
	end := len(p) > 0 && p[len(p)-1] == '$'
	if end {
		p = p[:len(p)-1]
	}
	switch {
	case start && end:
		return elem == p
	case start:
		return len(elem) >= len(p) && elem[:len(p)] == p
	case end:
		return len(elem) >= len(p) && elem[len(elem)-len(p):] == p
	}
	return contains(elem, p)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Test runs one top-level test.
func (m *M) Test(name string, f func(*T)) {
	var t T
	t.m = m
	t.setName(nil, name)
	m.ran = true
	t.run(f)
}

// Benchmark runs one benchmark, raising b.N until it runs for a second.
func (m *M) Benchmark(name string, f func(*B)) {
	if !m.header {
		fmt.Printf("goos: linux\ngoarch: amd64\npkg: %s\n", m.pkg)
		m.header = true
	}
	var b B
	b.m = m
	b.setName(nil, name)
	m.ran = true
	fmt.Printf("=== RUN   %s\n", b.Name())
	b.start = nanotime()
	n := 1
	ok := b.runN(f, n)
	for ok && b.elapsed < benchTime && n < maxN {
		prev := n
		elapsed := b.elapsed
		if elapsed <= 0 {
			elapsed = 1
		}
		n = int(int64(benchTime) * int64(prev) / elapsed)
		n += n / 5
		n = min(n, 100*prev)
		n = max(n, prev+1)
		n = min(n, maxN)
		ok = b.runN(f, n)
	}
	if ok {
		fmt.Printf("%-*s\t%8d\t", m.nameWidth, b.Name(), b.N)
		printNsPerOp(float64(b.elapsed) / float64(b.N))
	}
	b.report()
}

// printNsPerOp prints a benchmark's time per operation as go test does.
func printNsPerOp(x float64) {
	switch {
	case x == 0 || x >= 999.95:
		fmt.Printf("%10.0f ns/op\n", x)
	case x >= 99.995:
		fmt.Printf("%12.1f ns/op\n", x)
	case x >= 9.9995:
		fmt.Printf("%13.2f ns/op\n", x)
	case x >= 0.99995:
		fmt.Printf("%14.3f ns/op\n", x)
	case x >= 0.099995:
		fmt.Printf("%15.4f ns/op\n", x)
	default:
		fmt.Printf("%16.5f ns/op\n", x)
	}
}

// Exit prints the summary line and returns the exit status for main.
func (m *M) Exit() int64 {
	if !m.ran {
		fmt.Printf("testing: warning: no tests to run\n")
	}
	if m.failed {
		fmt.Printf("FAIL\n")
		return 1
	}
	fmt.Printf("PASS\n")
	return 0
}

// common is the state shared by T and B.
type common struct {
	m       *M
	parent  *common
	name    [maxName]byte
	nameLen int
	depth   int
	failed  bool
	skipped bool
	start   int64
}

// setName sets c's name to its parent's name, a slash, and elem with
// spaces replaced by underscores, as go test names subtests.
func (c *common) setName(parent *common, elem string) {
	n := 0
	if parent != nil {
		n = copy(c.name[:], parent.name[:parent.nameLen])
		if n < maxName {
			c.name[n] = '/'
			n++
		}
	}
	// elem may be cut at maxName: not in the middle of a rune, since Name
	// views the bytes as a string, which holds valid UTF-8.
	limit := len(elem)
	if limit > maxName-n {
		limit = maxName - n
		for limit > 0 && elem[limit]&0xC0 == 0x80 {
			limit--
		}
	}
	for i := 0; i < limit; i++ {
		ch := elem[i]
		if ch == ' ' {
			ch = '_'
		}
		c.name[n] = ch
		n++
	}
	c.nameLen = n
}

// Name returns the name of the running test or benchmark.
func (c *common) Name() string {
	// Valid UTF-8: the parent's name, a slash and elem cut at a rune
	// boundary (setName).
	return unsafe.String(&c.name[0], c.nameLen)
}

// Fail marks the test as failed and lets it continue.
func (c *common) Fail() { c.failed = true }

// Failed reports whether the test has failed.
func (c *common) Failed() bool { return c.failed }

// FailNow marks the test as failed and stops it with rt.Goexit: the
// deferred calls of the test function, and of the functions it is in, run.
// Later tests still run. FailNow must be called from the task running the
// test, not from a task the test started, which it would end instead.
func (c *common) FailNow() {
	c.failed = true
	rt.Goexit()
}

// SkipNow marks the test as skipped and stops it as FailNow does.
func (c *common) SkipNow() {
	c.skipped = true
	rt.Goexit()
}

// Skipped reports whether the test was skipped.
func (c *common) Skipped() bool { return c.skipped }

// Helper marks the calling function as a test helper. Messages carry no
// source position in Plutonium, so it has nothing to do.
func (c *common) Helper() {}

// Log formats its arguments with spaces between them and records the
// text in the test's output.
func (c *common) Log(args ...any) {
	var buf [msgSize]byte
	c.log(sprintln(&buf, args))
}

// Logf formats its arguments as Printf does and records the text in the
// test's output.
func (c *common) Logf(format string, args ...any) {
	var buf [msgSize]byte
	c.log(sprintf(&buf, format, args))
}

// Error is Log followed by Fail.
func (c *common) Error(args ...any) {
	var buf [msgSize]byte
	c.log(sprintln(&buf, args))
	c.Fail()
}

// Errorf is Logf followed by Fail.
func (c *common) Errorf(format string, args ...any) {
	var buf [msgSize]byte
	c.log(sprintf(&buf, format, args))
	c.Fail()
}

// Fatal is Log followed by FailNow.
func (c *common) Fatal(args ...any) {
	var buf [msgSize]byte
	c.log(sprintln(&buf, args))
	c.FailNow()
}

// Fatalf is Logf followed by FailNow.
func (c *common) Fatalf(format string, args ...any) {
	var buf [msgSize]byte
	c.log(sprintf(&buf, format, args))
	c.FailNow()
}

// Skip is Log followed by SkipNow.
func (c *common) Skip(args ...any) {
	var buf [msgSize]byte
	c.log(sprintln(&buf, args))
	c.SkipNow()
}

// Skipf is Logf followed by SkipNow.
func (c *common) Skipf(format string, args ...any) {
	var buf [msgSize]byte
	c.log(sprintf(&buf, format, args))
	c.SkipNow()
}

// sprintln formats args as fmt.Sprintln does into buf, as much as fits,
// and returns the text.
func sprintln(buf *[msgSize]byte, args []any) []byte {
	f := vec.NewFixed(buf[:])
	fmt.Appendln(&f, args...)
	return f.Slice()
}

// sprintf formats args as fmt.Sprintf does into buf, as much as fits, and
// returns the text.
func sprintf(buf *[msgSize]byte, format string, args []any) []byte {
	f := vec.NewFixed(buf[:])
	fmt.Appendf(&f, format, args...)
	return f.Slice()
}

// log writes a message as an indented line of the test's output, with
// continuation lines indented further, as go test prints them.
func (c *common) log(msg []byte) {
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	var buf [msgSize + 256]byte
	// One byte is kept for the final newline.
	out := vec.NewFixed(buf[:len(buf)-1])
	vec.AppendString(&out, "    ")
	for _, ch := range msg {
		if ch == '\n' {
			vec.AppendString(&out, "\n        ")
			continue
		}
		out.Push(ch)
	}
	n := out.Len()
	buf[n] = '\n'
	os.Stdout.Write(buf[:n+1])
}

// report prints the result line of a finished test.
func (c *common) report() {
	status := "PASS"
	if c.failed {
		status = "FAIL"
		c.m.failed = true
		if c.parent != nil {
			c.parent.failed = true
		}
	} else if c.skipped {
		status = "SKIP"
	}
	secs := float64(nanotime()-c.start) / 1e9
	fmt.Printf("--- %s: %s (%.2fs)\n", status, c.Name(), secs)
}

// T is the state of one test or subtest.
type T struct {
	common
}

// run runs f as the test t, announcing it first so that a crash is
// attributed to it.
func (t *T) run(f func(*T)) {
	fmt.Printf("=== RUN   %s\n", t.Name())
	t.start = nanotime()
	rt.CatchGoexit(func() { f(t) })
	t.report()
}

// Run runs f as a subtest of t called name and reports whether it
// passed. A subtest that the -run pattern does not select counts as
// passed.
func (t *T) Run(name string, f func(t *T)) bool {
	var sub T
	sub.m = t.m
	sub.parent = &t.common
	sub.depth = t.depth + 1
	sub.setName(&t.common, name)
	elem := ""
	if t.nameLen < maxName {
		elem = sub.Name()[t.nameLen+1:]
	}
	if !t.m.matches(sub.depth, elem) {
		return true
	}
	sub.run(f)
	return !sub.failed
}

// B is the state of one benchmark.
type B struct {
	common
	N          int
	timerOn    bool
	timerStart int64
	elapsed    int64
}

// runN runs the benchmark function once with b.N set to n and reports
// whether it finished without failing or skipping.
func (b *B) runN(f func(*B), n int) bool {
	b.N = n
	b.elapsed = 0
	b.timerOn = false
	b.StartTimer()
	rt.CatchGoexit(func() { f(b) })
	b.StopTimer()
	return !b.failed && !b.skipped
}

// StartTimer starts timing. A benchmark's timer runs while it starts.
func (b *B) StartTimer() {
	if !b.timerOn {
		b.timerStart = nanotime()
		b.timerOn = true
	}
}

// StopTimer stops timing, for setup the result should not include.
func (b *B) StopTimer() {
	if b.timerOn {
		b.elapsed += nanotime() - b.timerStart
		b.timerOn = false
	}
}

// ResetTimer discards the time measured so far.
func (b *B) ResetTimer() {
	if b.timerOn {
		b.timerStart = nanotime()
	}
	b.elapsed = 0
}

// ReportAllocs asks for allocation statistics. Plutonium has no
// allocation counters, so it does nothing.
func (b *B) ReportAllocs() {}

type timespec struct {
	sec, nsec int64
}

const clockMonotonic = 1

// nanotime returns the monotonic clock in nanoseconds.
func nanotime() int64 {
	var ts timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, clockMonotonic, uintptr(unsafe.Pointer(&ts)), 0)
	return ts.sec*1000000000 + ts.nsec
}
