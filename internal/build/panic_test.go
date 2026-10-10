package build_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/testguard"
)

// hostedPanicSource is a hosted program with its own panic handler, which
// reports what it is told about the panic and exits with status 3. The
// scenario constant, set by the test, picks how it panics.
const hostedPanicSource = `package main

import (
	"pluto/os"
	"pluto/rt"
)

// errWriter is a Writer of its own, to show that PrintTrace takes any.
type errWriter struct{}

func (errWriter) Write(b []byte) (int, error) { return os.Stderr.Write(b) }

//pu:panic
func onPanic(p *rt.Panic) {
	println("handler: message:", p.Message)
	if s, ok := p.Value.(string); ok {
		println("handler: value:", s)
	}
	println("handler: cpu", p.CPU, "fault", int(p.Fault.Kind), "signal", p.Fault.Signal, "addr", p.Fault.Addr)
	if scenario == "nested" {
		var xs []int
		println(xs[1])
	}
	p.PrintTrace(errWriter{})
	os.Exit(3)
}

type node struct{ next *node }

//pu:noinline
func get(xs []int, i int) int { return xs[i] }

//pu:noinline
func second(n *node) *node { return n.next.next }

func main() {
	println("start")
	switch scenario {
	case "value", "nested":
		panic("boom")
	case "index":
		println(get([]int{1, 2, 3}, 5))
	case "nil":
		println(second(&node{}) == nil)
	}
}
`

// TestHostedPanicHandler runs a hosted program whose own //pu:panic
// handler replaces the default one: it hears about an explicit panic, a
// runtime error and a hardware fault, and a panic inside it goes to the
// runtime's last resort.
func TestHostedPanicHandler(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct {
		scenario string
		status   int
		want     string // a regular expression; FILE is the program's trimmed path
	}{
		{"value", 3, `start
handler: message: boom
handler: value: boom
handler: cpu 0 fault 0 signal 0 addr 0
    main\.main \(FILE:40\)
`},
		{"index", 3, `start
handler: message: runtime error: index out of range \[5\] with length 3
handler: cpu 0 fault 0 signal 0 addr 0
    main\.get \(FILE:31\)
    main\.main \(FILE:42\)
`},
		{"nil", 3, `start
handler: message: runtime error: invalid memory address or nil pointer dereference
handler: cpu 0 fault 1 signal 11 addr 0
    main\.second \(FILE:34\)
    main\.main \(FILE:44\)
`},
		{"nested", 2, `start
handler: message: boom
handler: value: boom
handler: cpu 0 fault 0 signal 0 addr 0
fatal: panic while handling a panic
`},
	}
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			src := hostedPanicSource + "\nconst scenario = \"" + c.scenario + "\"\n"
			_, out, status := buildFiles(t, map[string][]byte{"main.p": []byte(src)}, build.Config{OptLevel: level})
			re := `\A` + strings.ReplaceAll(c.want, "FILE", `\./main\.p`) + `\z`
			if status != c.status || !regexp.MustCompile(re).MatchString(out) {
				t.Errorf("%s -O%d: status %d, output:\n%s\nwant status %d and a match for\n%s", c.scenario, level, status, out, c.status, re)
			}
		}
	}
}

// TestHostedPanicHandlerPipe checks why a panic handler may write to
// os.Stderr, as TestHostedPanicHandler's does: File.Write reaches the
// poller only through the ioWaiter interface, which only os.Pipe fills in.
// Without a pipe the program has no ioWaiter to call and builds. With one,
// the interrupt-context check follows that interface call into the
// poller's wait and reports the handler, since it cannot tell which File
// the handler writes to; the message names the call into the library, not
// how the library goes on to wait.
func TestHostedPanicHandlerPipe(t *testing.T) {
	testguard.Parallel(t)
	const handler = `package main

import (
	"pluto/os"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	os.Stderr.WriteString("panicked\n")
	os.Exit(3)
}

func main() {
	%s
	panic("boom")
}
`
	_, out, status := buildFiles(t, map[string][]byte{"main.p": []byte(strings.Replace(handler, "%s", "println(\"no pipe\")", 1))}, build.Config{})
	if status != 3 || out != "no pipe\npanicked\n" {
		t.Errorf("without a pipe: status %d, output %q; want 3, %q", status, out, "no pipe\npanicked\n")
	}

	err := buildError(t, strings.Replace(handler, "%s", "r, w, _ := os.Pipe()\n\tr.Close()\n\tw.Close()", 1))
	if err == nil {
		t.Fatal("built a panic handler that may wait in the poller")
	}
	var b strings.Builder
	diag.Print(&b, err, false)
	msg := strings.Join(strings.Fields(b.String()), " ")
	for _, want := range []string{
		"main.p:10:2: onPanic is the panic handler, so it must not wait",
		"onPanic waits on os.Stderr (main.p:10).",
		"The path goes through an os.File.",
		"Write to rt.Console{} instead of os.Stderr in a panic or interrupt handler; os.File writes may wait for the poller: p.Print(rt.Console{})",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not contain %q:\n%s", want, b.String())
		}
	}
}

// TestHostedPanicConsolePipe checks that the runtime's own console output
// does not reach the poller: a program with a pipe, puroutines (so the
// hosted scheduler's Wake is checked) and a File converted to rt.Writer
// builds, with a panic handler that prints through rt.Console. print,
// println and Panic.Print with a Console call Console.Write directly
// rather than through the Writer interface, which counts as calling every
// Writer, File's Write among them.
func TestHostedPanicConsolePipe(t *testing.T) {
	testguard.Parallel(t)
	src := []byte(`package main

import (
	"pluto/os"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	println("handler:", p.Message)
	p.Print(rt.Console{})
	os.Exit(3)
}

func emit(w rt.Writer, b []byte) {
	w.Write(b)
}

func writer(w *os.File, done chan int) {
	w.WriteString("hi\n")
	w.Close()
	done <- 1
}

func main() {
	r, w, _ := os.Pipe()
	done := make(chan int)
	go writer(w, done)
	var buf [8]byte
	n, _ := r.Read(buf[:])
	<-done
	r.Close()
	emit(os.Stderr, buf[:n])
	println(n)
	panic("boom")
}
`)
	want := "hi\n3\nhandler: boom\npanic: boom\n    main.main (./main.p:35)\n"
	for _, level := range []int{0, 2} {
		_, out, status := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: level})
		if status != 3 || out != want {
			t.Errorf("-O%d: status %d, output %q; want 3, %q", level, status, out, want)
		}
	}
}

// TestHostedDefaultPanic checks that a hosted program without a panic
// handler still reports a panic as before: the message and the trace on
// standard error, then exit status 2.
func TestHostedDefaultPanic(t *testing.T) {
	testguard.Parallel(t)
	src := []byte("package main\n\nfunc main() {\n\tpanic(\"boom\")\n}\n")
	_, out, status := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{})
	want := "panic: boom\n    main.main (./main.p:4)\n"
	if status != 2 || out != want {
		t.Errorf("status %d, output %q; want 2, %q", status, out, want)
	}
}

// TestFreestandingPanicHandlerRequired checks that a freestanding build
// without a panic handler does not build.
func TestFreestandingPanicHandlerRequired(t *testing.T) {
	testguard.Parallel(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, kernel := range []bool{false, true} {
		cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "k.o"), Stdlib: plutonium.Stdlib(), Freestanding: !kernel, Kernel: kernel, CompileOnly: true}
		err := build.Build(cfg)
		if err == nil || !strings.Contains(err.Error(), "no panic handler is defined") {
			t.Errorf("kernel %v: err = %v", kernel, err)
		}
	}
}
