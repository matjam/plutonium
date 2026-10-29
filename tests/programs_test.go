package tests

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The tests in this file build programs with pluto and run them: the
// programs of testdata/run, testdata/lib, testdata/plutonium,
// testdata/trace, testdata/cabi and testdata/kernel, and the examples.

// buildProgram builds the main package in dir with the flags of pluto build
// and returns the executable's path. A build that fails fails the test.
func buildProgram(t *testing.T, dir string, flags ...string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "prog")
	args := append([]string{"build"}, flags...)
	args = append(args, "-o", exe, dir)
	if out, status := runPluto(t, ".", nil, args...); status != 0 {
		t.Fatalf("pluto %s: exit status %d\n%s", strings.Join(args, " "), status, out)
	}
	return exe
}

// buildSource writes files to a new directory and builds it with flags. It
// returns the directory and the executable's path.
func buildSource(t *testing.T, files map[string][]byte, flags ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, buildProgram(t, dir, flags...)
}

// optFlag is the -O flag of level.
func optFlag(level int) string { return "-O" + strconv.Itoa(level) }

// runExe runs exe with env added to its environment and returns its output
// and exit status. If split is set the output is the standard output
// followed by the standard error, else the two as they were interleaved. A
// death by signal fails the test.
func runExe(t *testing.T, exe string, split bool, env ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if !split {
		cmd.Stderr = &stdout
	}
	err := cmd.Run()
	status := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = exitErr.ExitCode()
		if status < 0 {
			t.Fatalf("program died: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
		}
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String() + stderr.String(), status
}

// compileAndRun builds src as the main package at level and runs it,
// returning its standard output followed by its standard error, and its
// exit status.
func compileAndRun(t *testing.T, src []byte, level int) (string, int) {
	t.Helper()
	_, exe := buildSource(t, map[string][]byte{"main.p": src}, optFlag(level))
	return runExe(t, exe, true)
}

// goldenProgram runs src at -O0 and -O2 as subtests named name/O0 and
// name/O2 and checks that each exits with status 0 and prints want.
func goldenProgram(t *testing.T, name string, src, want []byte) {
	for _, level := range []int{0, 2} {
		t.Run(name+"/"+strings.TrimPrefix(optFlag(level), "-"), func(t *testing.T) {
			t.Parallel()
			got, status := compileAndRun(t, src, level)
			if status != 0 {
				t.Errorf("exit status %d", status)
			}
			if got != string(want) {
				t.Errorf("output differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// TestRun compiles and runs every program in testdata/run and compares its
// output with the .out file next to it. The programs are written in the
// common subset of Go and Plutonium, so -update produces the expected output
// by running them with the Go toolchain (see goprog_test.go). A program may
// import pluto/fmt, which Go runs as its fmt.
func TestRun(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("..", "testdata", "run", "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no programs in testdata/run")
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".p")
		golden := strings.TrimSuffix(file, ".p") + ".out"
		if *update {
			out, err := runWithGo(t, file)
			if err != nil {
				t.Errorf("%s: go run: %v\n%s", name, err, out)
				continue
			}
			if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Errorf("%s: %v (run with -update to create it)", name, err)
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		goldenProgram(t, name, src, want)
	}
}

// TestLib compiles and runs every program in testdata/lib, which exercise
// the standard library, and compares the output with the .out file next to
// it. These programs import pluto/... packages, so Go cannot run them as
// they are; their expected output was checked against Go running the same
// code with the imports pointed at Go's packages (or at shims for
// Plutonium-only APIs), or by hand where no Go equivalent exists.
func TestLib(t *testing.T) {
	t.Parallel()
	runGoldenDir(t, "lib")
}

// TestPlutonium compiles and runs every program in testdata/plutonium,
// which exercise language features Go does not have (enums, type
// constructor expressions). Go cannot run them, so their .out files were
// checked by hand.
func TestPlutonium(t *testing.T) {
	t.Parallel()
	runGoldenDir(t, "plutonium")
}

// runGoldenDir builds and runs each testdata/<dir>/*.p at -O0 and -O2 and
// compares its output with the .out file next to it.
func runGoldenDir(t *testing.T, dir string) {
	files, err := filepath.Glob(filepath.Join("..", "testdata", dir, "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no programs in testdata/%s", dir)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".p")
		want, err := os.ReadFile(strings.TrimSuffix(file, ".p") + ".out")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		goldenProgram(t, name, src, want)
	}
}

// TestBuilderAllocator checks the strings.Builder misuses that stop the
// program: a Builder gets its allocator once, so a second Init panics.
// testdata/lib/strings.p covers the uses that work.
func TestBuilderAllocator(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, want string }{
		{"Init twice", "var b strings.Builder\n\tb.Init()\n\tb.Init()", "panic: strings.Builder.Init: the Builder already has an allocator\n"},
		{"Init after growing", "var b strings.Builder\n\tb.WriteString(\"x\")\n\tb.Init()", "panic: strings.Builder.Init: the Builder already has an allocator\n"},
		{"Init with nil", "var b strings.Builder\n\tb.Init(nil)", "panic: strings.Builder.Init: nil allocator\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "package main\n\nimport \"pluto/strings\"\n\nfunc main() {\n\t" + c.body + "\n\tprintln(\"unreachable\")\n}\n"
			out, status := compileAndRun(t, []byte(src), 0)
			if status != 2 || !strings.HasPrefix(out, c.want) || strings.Contains(out, "unreachable") {
				t.Errorf("exit status %d, output %q; want 2 and %q first", status, out, c.want)
			}
		})
	}
}

// joinAttempts is how many runs of a program in a row may take the abandon
// path of section 15.4 before the test fails.
const joinAttempts = 30

// abandonMark is what the debug allocator prints instead of its leak report
// when main returns with tasks still running.
const abandonMark = "still running at exit; the leak report is skipped"

// runDebugAlloc runs exe, built with --alloc=debug, with PLUTO_DEBUGALLOC
// set to settings and returns its standard output followed by its standard
// error, and its exit status. A run in which main returned with tasks still
// running is run again, because a program that joins its tasks with a
// WaitGroup or a channel can take that path when the machine is loaded
// (decision D31). The run returned is one that accounted exactly.
func runDebugAlloc(t *testing.T, exe, settings string) (string, int) {
	t.Helper()
	var out string
	var status int
	for range joinAttempts {
		out, status = runExe(t, exe, true, "PLUTO_DEBUGALLOC="+settings)
		if !strings.Contains(out, abandonMark) {
			return out, status
		}
	}
	t.Fatalf("%d runs in a row took the abandon path of section 15.4 (the tasks never retired); last output %q, status %d", joinAttempts, out, status)
	return out, status
}

// TestExamples builds and runs every program in examples/ and checks its
// output against examples/<name>/expected (the exact output) and its exit
// status against a first line of the form "exit N" in that file, if any.
func TestExamples(t *testing.T) {
	t.Parallel()
	dirs, err := filepath.Glob(filepath.Join("..", "examples", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		src, err := os.ReadFile(filepath.Join(dir, "main.p"))
		if err != nil {
			continue
		}
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(filepath.Join(dir, "expected"))
			if err != nil {
				t.Fatalf("missing expected output: %v", err)
			}
			wantStatus := 0
			wantOut := string(want)
			if first, rest, ok := strings.Cut(wantOut, "\n"); ok && strings.HasPrefix(first, "exit ") {
				wantStatus, _ = strconv.Atoi(strings.TrimPrefix(first, "exit "))
				wantOut = rest
			}
			got, status := compileAndRun(t, src, 2)
			if status != wantStatus {
				t.Errorf("exit status %d, want %d", status, wantStatus)
			}
			if got != wantOut {
				t.Errorf("output differs\n--- got ---\n%s--- want ---\n%s", got, wantOut)
			}

			// With the debug allocator as the default allocator the
			// output is the same: the example misuses no memory.
			_, exe := buildSource(t, map[string][]byte{"main.p": src}, "-O2", "--alloc", "debug")
			got, status = runDebugAlloc(t, exe, "leaks=off")
			if status != wantStatus {
				t.Errorf("--alloc=debug: exit status %d, want %d", status, wantStatus)
			}
			if got != wantOut {
				t.Errorf("--alloc=debug: output differs\n--- got ---\n%s--- want ---\n%s", got, wantOut)
			}
		})
	}
}

var pcPattern = regexp.MustCompile(`pc=0x[0-9a-f]+`)

// buildAndRun builds files with flags and runs the program. It returns the
// directory, the program's combined output and its status.
func buildAndRun(t *testing.T, files map[string][]byte, flags ...string) (string, string, int) {
	t.Helper()
	dir, exe := buildSource(t, files, flags...)
	out, status := runExe(t, exe, false)
	return dir, out, status
}

// TestTraces runs each program in testdata/trace, which panics, and checks
// the message and stack trace it prints against the .out file next to it,
// where PC stands for a code address. The program is main.p of a package
// outside any module, so its frames name ./main.p wherever it was built.
// Traces must be the same at -O0 and -O2: inlined calls are reported as
// frames of their own.
func TestTraces(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("..", "testdata", "trace", "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".p")
		want, err := os.ReadFile(strings.TrimSuffix(file, ".p") + ".out")
		if os.IsNotExist(err) {
			continue // checked by a test of its own
		}
		if err != nil {
			t.Fatal(err)
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range []int{0, 2} {
			t.Run(name+"/"+strings.TrimPrefix(optFlag(level), "-"), func(t *testing.T) {
				t.Parallel()
				_, got, status := buildAndRun(t, map[string][]byte{"main.p": src}, optFlag(level))
				if status != 2 {
					t.Errorf("exit status %d, want 2", status)
				}
				got = pcPattern.ReplaceAllString(got, "pc=PC")
				if got != string(want) {
					t.Errorf("output differs\n--- got ---\n%s--- want ---\n%s", got, want)
				}
			})
		}
	}
}

// TestTraceStripped checks that a program built with -s, which has neither
// DWARF nor a symbol table, still prints a symbolised trace.
func TestTraceStripped(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "testdata", "trace", "index.p"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"main.p": src}
	_, out, status := buildAndRun(t, files, "-O2", "-s")
	if status != 2 || !strings.Contains(out, "    main.level3 (./main.p:4)\n") {
		t.Errorf("status %d, output:\n%s", status, out)
	}

	// --trimpath=false: the program's absolute path.
	dir, out, status := buildAndRun(t, files, "-O2", "--trimpath=false")
	if status != 2 || !strings.Contains(out, "    main.level3 ("+filepath.Join(dir, "main.p")+":4)\n") {
		t.Errorf("--trimpath=false: status %d, output:\n%s", status, out)
	}

	// -trace=false: the message alone.
	_, out, status = buildAndRun(t, files, "-O2", "--trace=false")
	if want := "7\npanic: runtime error: index out of range [4] with length 4\n"; status != 2 || out != want {
		t.Errorf("-trace=false: status %d, output:\n%s", status, out)
	}

	// -trace=false with deferred calls: the table keeps what the panic
	// needs to run them, and still prints no trace.
	src, err = os.ReadFile(filepath.Join("..", "testdata", "trace", "deferfault.p"))
	if err != nil {
		t.Fatal(err)
	}
	_, out, status = buildAndRun(t, map[string][]byte{"main.p": src}, "-O2", "--trace=false")
	out = pcPattern.ReplaceAllString(out, "pc=PC")
	if want := "iterator deferred\nsum deferred, total = 3\nmain deferred\n" +
		"panic: runtime error: invalid memory address or nil pointer dereference\n" +
		"[signal SIGSEGV: segmentation violation addr=0x8 pc=PC]\n"; status != 2 || out != want {
		t.Errorf("-trace=false with defers: status %d, output:\n%s", status, out)
	}
}

// TestTraceStackOverflow checks that running into the stack's guard page is
// reported as a stack overflow, with a trace cut short after 64 frames.
func TestTraceStackOverflow(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "testdata", "trace", "overflow.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		_, got, status := buildAndRun(t, map[string][]byte{"main.p": src}, optFlag(level))
		if status != 2 {
			t.Errorf("O%d: exit status %d, want 2", level, status)
		}
		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		frame := "    main.recurse (./main.p:"
		ok := len(lines) == 2+64+1 &&
			// The size is the process stack's resource limit (ulimit -s).
			strings.HasPrefix(lines[0], "panic: runtime error: stack overflow (main's stack is ") &&
			strings.HasSuffix(lines[0], "; ask for a larger one with //pu:stack on func main)") &&
			strings.HasPrefix(lines[1], "[signal SIGSEGV") &&
			strings.HasPrefix(lines[2], frame) &&
			lines[3] == frame+"6)" && lines[65] == frame+"6)" &&
			lines[66] == "    ...additional frames elided..."
		if !ok {
			t.Errorf("O%d: unexpected output:\n%s", level, got)
		}
	}
}

// TestTraceTaskOverflow checks that a task that runs into the
// guard page below its stack, on a worker thread with its own signal
// stack, is reported as a stack overflow with the task's trace.
func TestTraceTaskOverflow(t *testing.T) {
	t.Parallel()
	src := `package main

import "pluto/rt"

func recurse(n int) int {
	var buf [64]int
	buf[n&63] = n
	return recurse(n+1) + buf[(n+1)&63]
}

var done = make(chan bool)

func run() {
	println(recurse(0))
	done <- true
}

func main() {
	rt.Go(run, 16<<10)
	<-done
}
`
	for _, level := range []int{0, 2} {
		_, got, status := buildAndRun(t, map[string][]byte{"main.p": []byte(src)}, optFlag(level))
		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		// The 16 KiB stack holds fewer than 64 frames: the trace reaches
		// the function rt.Go started.
		frame := "    main.recurse (./main.p:"
		ok := status == 2 && len(lines) > 10 &&
			lines[0] == "panic: runtime error: stack overflow (the task's stack is 24 KiB; start the task with rt.Go(f, size) for a larger one)" &&
			strings.HasPrefix(lines[1], "[signal SIGSEGV") &&
			lines[2] == "task 2:" &&
			strings.HasPrefix(lines[3], frame) &&
			lines[len(lines)-1] == "    main.run (./main.p:14)"
		if !ok {
			t.Errorf("O%d: status %d, unexpected output:\n%s", level, status, got)
		}
	}
}

// TestTraceFreestanding checks that a freestanding build, whose runtime has
// no operating system, prints its trace through the console the program
// registers and stops through its panic handler.
func TestTraceFreestanding(t *testing.T) {
	t.Parallel()
	src := filepath.Join("..", "testdata", "trace", "freestanding")
	files := map[string][]byte{}
	for _, name := range []string{"main.p", "start_amd64.s"} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
	}
	want, err := os.ReadFile(filepath.Join(src, "expected"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		_, got, status := buildAndRun(t, files, optFlag(level), "--mode", "freestanding", "--gencheck=false")
		if status != 3 {
			t.Errorf("O%d: exit status %d, want 3 (from the panic handler)", level, status)
		}
		if got != string(want) {
			t.Errorf("O%d: output differs\n--- got ---\n%s--- want ---\n%s", level, got, want)
		}
	}
}

// bootKernel boots image under QEMU and returns what it wrote to the serial
// port. The kernel leaves through isa-debug-exit with 0, which QEMU reports
// as status (0 << 1) | 1; any other end fails the test.
func bootKernel(t *testing.T, qemu, image string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, qemu, "-kernel", image, "-serial", "stdio", "-display", "none",
		"-monitor", "none", "-no-reboot", "-m", "64M",
		"-device", "isa-debug-exit,iobase=0xf4,iosize=0x04")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("qemu: %v (want exit status 1)\nserial: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// needQEMU returns the path of qemu-system-x86_64, skipping the test if it
// is not installed.
func needQEMU(t *testing.T) string {
	t.Helper()
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		t.Skip("qemu-system-x86_64 is not installed")
	}
	return qemu
}

// TestKernelScheduler boots testdata/kernel/sched, a kernel-mode image
// that binds its own scheduler (a per-CPU round-robin run queue with
// preemption from the PIT timer interrupt), under QEMU, and checks that
// tasks, channels, select and pluto/sync run on it: a spinning
// task with no calls is preempted, and the timer handler wakes a
// parked task with a non-blocking channel send.
func TestKernelScheduler(t *testing.T) {
	t.Parallel()
	qemu := needQEMU(t)
	dir := filepath.Join("..", "testdata", "kernel", "sched")
	const want = "sum of squares 385\n" +
		"map 4 735 721 2 2\n" +
		"spinner preempted by the timer\n" +
		"ticks from the interrupt handler true true true\n" +
		"b 42\n" +
		"slept true\n" +
		"woke in order 1 2 3\n" +
		"select timed out\n" +
		"ticker true true\n" +
		"cubes 36\n" +
		"waited 3\n" +
		"done\n"
	for _, level := range []int{0, 2} {
		t.Run(strings.TrimPrefix(optFlag(level), "-"), func(t *testing.T) {
			t.Parallel()
			// The ELF image builds too.
			buildProgram(t, dir, optFlag(level), "--mode", "kernel", "--gencheck=false")
			image := buildProgram(t, dir, optFlag(level), "--mode", "kernel", "--gencheck=false", "--format", "binary")
			if got := bootKernel(t, qemu, image); got != want {
				t.Errorf("serial output differs\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

// TestKernelInterrupts boots the freestanding kernel in testdata/kernel/irq
// under QEMU. It installs two //pu:interrupt handlers and enters them with
// int $0x80 and with a general protection fault (which pushes an error
// code), then reports over the serial port whether every register survived,
// the stack alignment inside the handlers, and the error code.
func TestKernelInterrupts(t *testing.T) {
	t.Parallel()
	qemu := needQEMU(t)
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "kernel", "irq"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "in handler: vector 0x80, cs 8\n" +
		"int 0x80: count 1 registers changed 0 stack mod 16 0 cs 8\n" +
		"float in handler x2: 3\n" +
		"#GP: count 1 error code 24 stack mod 16 0\n"
	for _, level := range []int{0, 2} {
		t.Run(strings.TrimPrefix(optFlag(level), "-"), func(t *testing.T) {
			t.Parallel()
			image := buildProgram(t, dir, optFlag(level), "--mode", "freestanding", "--gencheck=false",
				"--ldscript", filepath.Join(dir, "kernel.ld"), "--format", "binary")
			if got := bootKernel(t, qemu, image); got != want {
				t.Errorf("serial output differs\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

// needCC skips the test if there is no C compiler.
func needCC(t *testing.T) {
	t.Helper()
	cc := strings.Fields(os.Getenv("CC"))
	if len(cc) == 0 {
		cc = []string{"cc"}
	}
	if _, err := exec.LookPath(cc[0]); err != nil {
		t.Skip("no C compiler")
	}
}

// TestCABI links testdata/cabi/main.p with cabi.c, compiled by the system C
// compiler, and checks calls in both directions under System V: scalars of
// every width, floats, structs of each class as arguments and results,
// memory-class structs, register exhaustion, several results, func values of
// C functions and C callbacks into //pu:export functions.
func TestCABI(t *testing.T) {
	t.Parallel()
	needCC(t)
	dir := filepath.Join("..", "testdata", "cabi")
	want, err := os.ReadFile(filepath.Join(dir, "expected"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		t.Run(strings.TrimPrefix(optFlag(level), "-"), func(t *testing.T) {
			t.Parallel()
			got, status := runExe(t, buildProgram(t, dir, optFlag(level)), false)
			if status != 0 {
				t.Errorf("exit status %d", status)
			}
			if got != string(want) {
				t.Errorf("output differs\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

// TestObjectFiles links a package's .o file as it is.
func TestObjectFiles(t *testing.T) {
	t.Parallel()
	needCC(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "add.c")
	if err := os.WriteFile(src, []byte("long add3(long a, long b, long c) { return a + b * 10 + c * 20; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cc", "-c", "-O2", "-fno-stack-protector", "-o", filepath.Join(dir, "add_amd64.o"), src).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	// A file-name tag that does not match leaves the object out.
	if err := os.WriteFile(filepath.Join(dir, "unused_arm64.o"), []byte("not an object"), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := "package main\n\n//pu:extern add3\nfunc add3(a, b, c int) int\n\nfunc main() int64 { return int64(add3(1, 2, 3)) }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, status := runExe(t, buildProgram(t, dir, "-O2"), false); status != 81 {
		t.Errorf("exit status %d, want 81", status)
	}
}
