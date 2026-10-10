package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
)

// A build mode for lowerModeError.
type mode int

const (
	hosted mode = iota
	freestanding
	kernel
)

// lowerModeError lowers src as a main package in the given build mode and
// returns the lowering error, or "" if it lowered cleanly.
func lowerModeError(t *testing.T, src string, m mode) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs(filepath.Join("..", "..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]bool{"linux": true, "amd64": true, "hosted": true}
	cfg := &lower.Config{}
	if m != hosted {
		tags = map[string]bool{"none": true, "freestanding": true, "amd64": true}
		cfg.Freestanding = true
		if m == kernel {
			tags["kernel"] = true
			cfg.Kernel = true
		}
	}
	prog, err := load.Load(&load.Config{Tags: tags, Stdlib: os.DirFS(lib)}, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := lower.Lower(cfg, prog); err != nil {
		var b strings.Builder
		diag.Print(&b, err, false)
		return b.String()
	}
	return ""
}

// containsText reports whether text contains want, with any run of white
// space in either counting as one space, so that wrapping does not matter.
func containsText(text, want string) bool {
	return strings.Contains(strings.Join(strings.Fields(text), " "), strings.Join(strings.Fields(want), " "))
}

// kernelHeader starts the kernel-mode test programs.
const kernelHeader = "package main\n\nimport (\n\t\"pluto/arch/x86\"\n\t\"pluto/rt\"\n)\n\nvar _ x86.InterruptFrame\nvar _ rt.Panic\n\n"

// goodHandler is a panic handler that never returns.
const goodHandler = "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tfor {\n\t\tx86.Hlt()\n\t}\n}\n\n"

// TestPanicHandlerDecl checks the rules on declaring the panic handler.
func TestPanicHandlerDecl(t *testing.T) {
	cases := []struct {
		name string
		mode mode
		src  string
		want []string // substrings of the error; none if it must compile
	}{
		{"kernel without", kernel, "func main() {}\n", []string{
			"kernel: no panic handler is defined\n",
			"    A kernel build has no operating system to report a crash to",
			"        //pu:panic\n        func onPanic(p *rt.Panic) { ... }\n",
			`(see "Panics in freestanding programs" in the Plutonium spec, section 8.1)`,
			`(run "pluto explain E2580" for a longer explanation)`}},
		{"freestanding without", freestanding, "func main() {}\n", []string{
			"freestanding: no panic handler is defined\n    A freestanding build has no operating system"}},
		{"hosted without", hosted, "func main() {}\n", nil},
		{"one", kernel, goodHandler + "func main() {}\n", nil},
		{"two", kernel, goodHandler + "//pu:panic\nfunc onPanic2(p *rt.Panic) {\n\tfor {\n\t}\n}\n\nfunc main() {}\n", []string{
			"main.p:19:6: onPanic2 is a second panic handler\n",
			"onPanic (main.p:12) is already\n    marked //pu:panic"}},
		{"two hosted", hosted, goodHandler + "//pu:panic\nfunc onPanic2(p *rt.Panic) {\n\tfor {\n\t}\n}\n\nfunc main() {}\n", []string{
			"onPanic2 is a second panic handler"}},
		{"signature", kernel, "//pu:panic\nfunc onPanic(p int) {\n\tfor {\n\t}\n}\n\nfunc main() {}\n", []string{
			"main.p:12:6: onPanic cannot be the panic handler: its signature is func(p int)\n",
			"        func onPanic(p *rt.Panic)\n"}},
		{"result", kernel, "//pu:panic\nfunc onPanic(p *rt.Panic) int {\n\tfor {\n\t}\n}\n\nfunc main() {}\n", []string{
			"its signature is func(p *rt.Panic) int"}},
		{"method", kernel, "type T struct{}\n\n//pu:panic\nfunc (T) onPanic(p *rt.Panic) {\n\tfor {\n\t}\n}\n\nfunc main() {}\n", []string{
			"onPanic cannot be the panic handler: it is a method"}},
		{"generic", kernel, "//pu:panic\nfunc onPanic[T any](p *rt.Panic) {\n\tfor {\n\t}\n}\n\nfunc main() {}\n", []string{
			"onPanic cannot be the panic handler: it is generic"}},
		{"no body", kernel, "//pu:panic\nfunc onPanic(p *rt.Panic)\n\nfunc main() {}\n", []string{
			"onPanic cannot be the panic handler: it has no body"}},
		{"interrupt", kernel, "//pu:panic\n//pu:interrupt\nfunc onPanic(p *rt.Panic) {\n\tfor {\n\t}\n}\n\nfunc main() {}\n", []string{
			"onPanic cannot be the panic handler: it is also an interrupt handler"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lowerModeError(t, kernelHeader+c.src, c.mode)
			if len(c.want) == 0 && got != "" {
				t.Fatalf("unexpected error:\n%s", got)
			}
			if len(c.want) > 0 && got == "" {
				t.Fatalf("compiled; want an error containing %q", c.want)
			}
			for _, w := range c.want {
				if !containsText(got, w) {
					t.Errorf("error does not contain %q:\n%s", w, got)
				}
			}
		})
	}
}

// TestPanicHandlerNoReturn checks that the panic handler cannot return.
func TestPanicHandlerNoReturn(t *testing.T) {
	const returns = "onPanic is the panic handler, so it must never return\n"
	cases := []struct {
		name, src string
		want      []string
	}{
		{"loop", goodHandler, nil},
		{"falls off", "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tx86.Hlt()\n}\n\n", []string{
			"main.p:14:1: " + returns,
			"    A panic stops the program: it does not unwind",
			"onPanic can return here (main.p:14).\n"}},
		{"one branch", "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tif p.CPU == 0 {\n\t\tfor {\n\t\t}\n\t}\n}\n\n", []string{
			"main.p:17:1: " + returns}},
		{"return statement", "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tfor {\n\t\tif p.CPU > 1 {\n\t\t\treturn\n\t\t}\n\t}\n}\n\n", []string{
			"main.p:15:4: " + returns}},
		{"noreturn directive", "//pu:noreturn\nfunc halt()\n\n//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tprintln(p.Message)\n\thalt()\n}\n\n", nil},
		{"inferred", "func stop() {\n\tfor {\n\t\tx86.Cli()\n\t\tx86.Hlt()\n\t}\n}\n\n" +
			"func die(n int) {\n\tif n > 0 {\n\t\tstop()\n\t}\n\tstop()\n}\n\n" +
			"//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tprintln(p.Message)\n\tdie(p.CPU)\n}\n\n", nil},
		{"inferred returns", "func stop(n int) {\n\tif n > 0 {\n\t\tfor {\n\t\t}\n\t}\n}\n\n" +
			"//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tstop(p.CPU)\n}\n\n", []string{returns}},
		// Recursion is not proof that a function never returns.
		{"recursion", "func spin(n int) {\n\tspin(n + 1)\n}\n\n" +
			"//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tspin(0)\n}\n\n", []string{returns}},
		{"panics", "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tpanic(\"again\")\n}\n\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lowerModeError(t, kernelHeader+c.src+"func main() {}\n", kernel)
			if len(c.want) == 0 && got != "" {
				t.Fatalf("unexpected error:\n%s", got)
			}
			if len(c.want) > 0 && got == "" {
				t.Fatalf("compiled; want an error containing %q", c.want)
			}
			for _, w := range c.want {
				if !containsText(got, w) {
					t.Errorf("error does not contain %q:\n%s", w, got)
				}
			}
		})
	}

	// Hosted programs may end the handler with os.Exit.
	const hostedSrc = "package main\n\nimport (\n\t\"pluto/os\"\n\t\"pluto/rt\"\n)\n\n" +
		"//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tprintln(p.Message)\n\tos.Exit(3)\n}\n\nfunc main() {}\n"
	if got := lowerModeError(t, hostedSrc, hosted); got != "" {
		t.Errorf("hosted handler ending in os.Exit: %s", got)
	}
	const hostedReturns = "package main\n\nimport \"pluto/rt\"\n\n" +
		"//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tprintln(p.Message)\n}\n\nfunc main() {}\n"
	if got := lowerModeError(t, hostedReturns, hosted); !strings.Contains(got, "main.p:8:1: "+returns) {
		t.Errorf("hosted handler that returns: %q", got)
	}
}
