package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// buildFiles writes files to a temporary directory, builds it with cfg's
// settings and runs it. It returns the directory, the program's combined
// output and its status.
func buildFiles(t *testing.T, files map[string][]byte, cfg build.Config) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Dir = dir
	cfg.Output = filepath.Join(dir, "prog")
	cfg.Stdlib = plutonium.Stdlib()
	if err := build.Build(&cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(cfg.Output)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	status := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = exitErr.ExitCode()
		if status < 0 {
			t.Fatalf("program died: %v\noutput: %s", err, out.String())
		}
	} else if err != nil {
		t.Fatal(err)
	}
	return dir, out.String(), status
}

var pcPattern = regexp.MustCompile(`pc=0x[0-9a-f]+`)

// TestTraces runs each program in testdata/trace, which panics, and checks
// the message and stack trace it prints against the .out file next to it,
// where PC stands for a code address. The program is main.p of a package
// outside any module, so its frames name ./main.p wherever it was built.
// Traces must be the same at -O0 and -O2: inlined calls are reported as
// frames of their own.
func TestTraces(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "trace", "*.p"))
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
			t.Run(name+"/O"+string(rune('0'+level)), func(t *testing.T) {
				_, got, status := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: level})
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
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trace", "index.p"))
	if err != nil {
		t.Fatal(err)
	}
	_, out, status := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: 2, Strip: true})
	if status != 2 || !strings.Contains(out, "    main.level3 (./main.p:4)\n") {
		t.Errorf("status %d, output:\n%s", status, out)
	}

	// --trimpath=false: the program's absolute path.
	dir, out, status := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: 2, NoTrimPath: true})
	if status != 2 || !strings.Contains(out, "    main.level3 ("+filepath.Join(dir, "main.p")+":4)\n") {
		t.Errorf("--trimpath=false: status %d, output:\n%s", status, out)
	}

	// -trace=false: the message alone.
	_, out, status = buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: 2, NoTrace: true})
	if want := "7\npanic: runtime error: index out of range [4] with length 4\n"; status != 2 || out != want {
		t.Errorf("-trace=false: status %d, output:\n%s", status, out)
	}

	// -trace=false with deferred calls: the table keeps what the panic
	// needs to run them, and still prints no trace.
	src, err = os.ReadFile(filepath.Join("..", "..", "testdata", "trace", "deferfault.p"))
	if err != nil {
		t.Fatal(err)
	}
	_, out, status = buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: 2, NoTrace: true})
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
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trace", "overflow.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		_, got, status := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: level})
		if status != 2 {
			t.Errorf("O%d: exit status %d, want 2", level, status)
		}
		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		frame := "    main.recurse (./main.p:"
		ok := len(lines) == 2+64+1 &&
			lines[0] == "panic: runtime error: stack overflow" &&
			strings.HasPrefix(lines[1], "[signal SIGSEGV") &&
			strings.HasPrefix(lines[2], frame) &&
			lines[3] == frame+"6)" && lines[65] == frame+"6)" &&
			lines[66] == "    ...additional frames elided..."
		if !ok {
			t.Errorf("O%d: unexpected output:\n%s", level, got)
		}
	}
}

// TestTracePuroutineOverflow checks that a puroutine that runs into the
// guard page below its stack, on a worker thread with its own signal
// stack, is reported as a stack overflow with the puroutine's trace.
func TestTracePuroutineOverflow(t *testing.T) {
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
		_, got, status := buildFiles(t, map[string][]byte{"main.p": []byte(src)}, build.Config{OptLevel: level})
		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		// The 16 KiB stack holds fewer than 64 frames: the trace reaches
		// the function rt.Go started.
		frame := "    main.recurse (./main.p:"
		ok := status == 2 && len(lines) > 10 &&
			lines[0] == "panic: runtime error: stack overflow" &&
			strings.HasPrefix(lines[1], "[signal SIGSEGV") &&
			lines[2] == "puroutine 2:" &&
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
	src := filepath.Join("..", "..", "testdata", "trace", "freestanding")
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
		_, got, status := buildFiles(t, files, build.Config{OptLevel: level, Freestanding: true})
		if status != 3 {
			t.Errorf("O%d: exit status %d, want 3 (from the panic handler)", level, status)
		}
		if got != string(want) {
			t.Errorf("O%d: output differs\n--- got ---\n%s--- want ---\n%s", level, got, want)
		}
	}
}
