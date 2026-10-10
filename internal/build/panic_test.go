package build_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
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

// TestHostedDefaultPanic checks that a hosted program without a panic
// handler still reports a panic as before: the message and the trace on
// standard error, then exit status 2.
func TestHostedDefaultPanic(t *testing.T) {
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
