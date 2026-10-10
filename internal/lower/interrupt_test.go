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
		{"HandlerAddr of a plain function", "func h(f *x86.InterruptFrame) {}\n\nfunc main() { _ = x86.HandlerAddr(h) }\n",
			"x86.HandlerAddr: argument must name a //pu:interrupt function"},
		{"HandlerAddr of a value", "func main() { _ = x86.HandlerAddr(3) }\n",
			"x86.HandlerAddr: argument must name a //pu:interrupt function"},
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

//pu:interrupt
func timer(f *x86.InterruptFrame) { n++ }

//pu:interrupt
func fault(f *x86.InterruptFrame, c uint64) { code = c }

func main() { println(x86.HandlerAddr(timer), x86.HandlerAddr(fault)) }
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
	}
}
