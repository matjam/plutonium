package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/ssa"
)

// lowerError lowers src as a main package and returns the lowering error,
// or "" if it lowered cleanly.
func lowerError(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs(filepath.Join("..", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{
		Tags:   map[string]bool{"linux": true, "amd64": true, "hosted": true},
		Stdlib: os.DirFS(lib),
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := lower.Lower(&lower.Config{}, prog); err != nil {
		return err.Error()
	}
	return ""
}

func TestInterruptErrors(t *testing.T) {
	const header = "package main\n\nimport (\n\t\"pluto/arch/x86\"\n\t\"unsafe\"\n)\n\nvar _ unsafe.Pointer\nvar _ x86.InterruptFrame\n\n"
	cases := []struct {
		name, src, want string
	}{
		{"results", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) int { return 0 }\n\nfunc main() {}\n",
			"interrupt handler h cannot have results"},
		{"no params", "//pu:interrupt\nfunc h() {}\n\nfunc main() {}\n",
			"interrupt handler h must take"},
		{"three params", "//pu:interrupt\nfunc h(f *x86.InterruptFrame, c, d uint64) {}\n\nfunc main() {}\n",
			"interrupt handler h must take"},
		{"frame not a pointer", "//pu:interrupt\nfunc h(f int) {}\n\nfunc main() {}\n",
			"first parameter must be a pointer to the interrupt frame"},
		{"bad error code", "//pu:interrupt\nfunc h(f unsafe.Pointer, code uint32) {}\n\nfunc main() {}\n",
			"error code must be uint64"},
		{"method", "type T struct{}\n\n//pu:interrupt\nfunc (T) h(f *x86.InterruptFrame) {}\n\nfunc main() {}\n",
			"method h cannot be an interrupt handler"},
		{"generic", "//pu:interrupt\nfunc h[T any](f *T) {}\n\nfunc main() {}\n",
			"generic function h cannot be an interrupt handler"},
		{"no body", "//pu:interrupt\nfunc h(f *x86.InterruptFrame)\n\nfunc main() {}\n",
			"//pu:interrupt function h must have a body"},
		{"inline", "//pu:interrupt\n//pu:inline\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() {}\n",
			"cannot be //pu:inline"},
		{"direct call", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { h(nil) }\n",
			"interrupt handler main.h cannot be called"},
		{"deferred call", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { defer h(nil) }\n",
			"interrupt handler main.h cannot be called"},
		{"func value", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { g := h; _ = g }\n",
			"interrupt handler main.h cannot be used as a func value"},
		{"InterruptGate of a plain function", "func h(f *x86.InterruptFrame) {}\n\nfunc main() { _ = x86.InterruptGate(h, 0x08, 0) }\n",
			"x86.InterruptGate: the first argument must name a //pu:interrupt function"},
		{"InterruptGate of a value", "func main() { _ = x86.InterruptGate(3, 0x08, 0) }\n",
			"x86.InterruptGate: the first argument must name a //pu:interrupt function"},
		{"InterruptGate of a func value", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { g := h; _ = x86.InterruptGate(g, 0x08, 0) }\n",
			"x86.InterruptGate: the first argument must name a //pu:interrupt function"},
		{"InterruptGate of a trap handler", "//pu:interrupt trap\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { _ = x86.InterruptGate(h, 0x08, 0) }\n",
			"x86.InterruptGate: h is a //pu:interrupt trap function; install it with x86.TrapGate"},
		{"TrapGate of an interrupt handler", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { _ = x86.TrapGate(h, 0x08, 0) }\n",
			"x86.TrapGate: h is a //pu:interrupt function, not a trap handler; install it with x86.InterruptGate"},
		{"TrapGate of a plain function", "func h(f *x86.InterruptFrame) {}\n\nfunc main() { _ = x86.TrapGate(h, 0x08, 0) }\n",
			"x86.TrapGate: the first argument must name a //pu:interrupt trap function"},
		{"a gate constructor as a func value", "func main() { g := x86.InterruptGate; _ = g }\n",
			"x86.InterruptGate cannot be used as a func value"},
		{"a deferred gate constructor with a function that is no handler", "func h(f *x86.InterruptFrame) {}\n\nfunc main() { defer x86.InterruptGate(h, 0x08, 0) }\n",
			"x86.InterruptGate cannot be deferred"},
		{"a deferred gate constructor with a handler", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { defer x86.InterruptGate(h, 0x08, 0) }\n",
			"x86.InterruptGate cannot be deferred"},
		{"a deferred gate constructor with main", "func main() { defer x86.InterruptGate(main, 0x08, 0) }\n",
			"x86.InterruptGate cannot be deferred"},
		{"a deferred trap gate constructor with nil", "func main() { defer x86.TrapGate(nil, 0x08, 0) }\n",
			"x86.TrapGate cannot be deferred"},
		{"a gate constructor started by a task statement", "//pu:interrupt\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { task x86.InterruptGate(h, 0x08, 0) }\n",
			"x86.InterruptGate cannot be started by a task statement"},
		{"a trap handler written with two spaces, installed with InterruptGate", "//pu:interrupt  trap\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { _ = x86.InterruptGate(h, 0x08, 0) }\n",
			"x86.InterruptGate: h is a //pu:interrupt trap function; install it with x86.TrapGate"},
		{"a trap handler written with a tab, installed with InterruptGate", "//pu:interrupt\ttrap\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { _ = x86.InterruptGate(h, 0x08, 0) }\n",
			"x86.InterruptGate: h is a //pu:interrupt trap function; install it with x86.TrapGate"},
		{"a trap handler written with two spaces, called", "//pu:interrupt  trap\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { h(nil) }\n",
			"interrupt handler main.h cannot be called"},
		{"a trap handler written with a tab, called", "//pu:interrupt\ttrap\nfunc h(f *x86.InterruptFrame) {}\n\nfunc main() { h(nil) }\n",
			"interrupt handler main.h cannot be called"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lowerError(t, header+c.src)
			if !strings.Contains(got, c.want) {
				t.Errorf("error %q, want it to contain %q", got, c.want)
			}
		})
	}
}

func TestInterruptHandlerLowering(t *testing.T) {
	prog := lowerSource(t, `package main

import "pluto/arch/x86"

var n, code uint64

// An exported name is an alias of the handler's code, for assembly.
//
//pu:interrupt
//pu:export isr_timer
func timer(f *x86.InterruptFrame) { n++ }

//pu:interrupt
func fault(f *x86.InterruptFrame, c uint64) { code = c }

var idt [2]x86.Gate

func main() {
	idt[0] = x86.InterruptGate(timer, 0x08, 0)
	idt[1] = x86.InterruptGate(fault, 0x08, 0)
}
`)
	for _, name := range []string{"main.timer", "main.fault"} {
		var f *ssa.Func
		for _, g := range prog.Funcs {
			if g.Name == name {
				f = g
			}
		}
		if f == nil {
			t.Fatalf("%s not lowered", name)
		}
		if f.Pragmas&ssa.PragmaInterrupt == 0 {
			t.Errorf("%s: not marked as an interrupt handler", name)
		}
		if len(f.Results) != 0 {
			t.Errorf("%s: results %v", name, f.Results)
		}
		if name == "main.timer" && (len(f.Exports) != 1 || f.Exports[0] != "isr_timer") {
			t.Errorf("%s: exports %v, want [isr_timer]", name, f.Exports)
		}
	}
}

// A trap handler is a trap handler however the white space between the
// directive and its argument is written: the lowering, the directive check
// and the race analysis read the directive the same way.
func TestTrapDirectiveWhiteSpace(t *testing.T) {
	for _, dir := range []string{"//pu:interrupt trap", "//pu:interrupt  trap", "//pu:interrupt\ttrap", "//pu:interrupt \t trap"} {
		prog := lowerSource(t, `package main

import "pluto/arch/x86"

var n uint64

`+dir+`
func h(f *x86.InterruptFrame) { n++ }

var idt [1]x86.Gate

func main() { idt[0] = x86.TrapGate(h, 0x08, 0) }
`)
		found := false
		for _, g := range prog.Funcs {
			if g.Name == "main.h" {
				found = true
				if g.Pragmas&ssa.PragmaInterrupt == 0 {
					t.Errorf("%q: main.h is not marked as an interrupt handler", dir)
				}
			}
		}
		if !found {
			t.Errorf("%q: main.h not lowered", dir)
		}
	}
}
