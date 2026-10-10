package lower_test

import (
	"strings"
	"testing"
)

// contextDecls are declarations the interrupt-context tests share: a lock
// whose Lock waits and whose TryLock does not, a function that waits, and
// an interrupt handler installed by main.
const contextDecls = `type Mutex struct{ held bool }

//pu:blocking
func (m *Mutex) Lock() { m.held = true }

func (m *Mutex) TryLock() bool {
	if m.held {
		return false
	}
	m.held = true
	return true
}

//pu:blocking
func wait()

var mu Mutex

func main() { _ = x86.HandlerAddr(keyboard) }

`

// TestInterruptContext checks that nothing an interrupt handler or the
// panic handler can reach may wait or start a puroutine.
func TestInterruptContext(t *testing.T) {
	const irqWait = "keyboard is an interrupt handler, so it must not wait\n" +
		"    Interrupt handlers run on top of whatever code the CPU was executing."
	cases := []struct {
		name, src string
		want      []string
	}{
		{"direct", "//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\twait()\n}\n", []string{
			"main.p:42:2: " + irqWait,
			"    keyboard calls wait (main.p:42), which may wait.\n",
			"    To hand work to normal code, use a non-blocking send:\n        select { case work <- item: default: }\n",
			`    (see "Interrupts" in the language spec)`}},
		{"method", "//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\tmu.Lock()\n}\n", []string{
			"main.p:42:2: " + irqWait,
			"keyboard waits on mu (main.p:42)."}},
		{"levels", "func readConfig() { load(1) }\n\nfunc load(n int) {\n\tif n > 0 {\n\t\tparse()\n\t}\n}\n\n" +
			"func parse() { mu.Lock() }\n\n" +
			"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\treadConfig()\n}\n", []string{
			"main.p:52:2: " + irqWait,
			"keyboard calls readConfig (main.p:52), which calls load (main.p:40),\n    which calls parse (main.p:44), which waits on mu (main.p:48)."}},
		{"method chain", "type dev struct{ m *Mutex }\n\nfunc (d *dev) reset() { d.m.Lock() }\n\nvar kbd = dev{m: &mu}\n\n" +
			"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\tkbd.reset()\n}\n", []string{
			"keyboard calls kbd.reset (main.p:48), which waits on d.m (main.p:42)."}},
		{"func value", "var hook func()\n\nfunc quick() {}\n\nfunc slow() { wait() }\n\nfunc init() {\n\thook = quick\n\thook = slow\n}\n\n" +
			"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\thook()\n}\n", []string{
			"keyboard calls hook (main.p:53), which may be slow, which calls wait\n    (main.p:44), which may wait.",
			"hook calls through a func value, so it counts as calling every function\n    of type func() whose value the program takes."}},
		{"interface", "type Locker interface{ Lock() }\n\nvar l Locker = &mu\n\n" +
			"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\tl.Lock()\n}\n", []string{
			"keyboard calls l.Lock (main.p:46), which may be (*Mutex).Lock, which may\n    wait.",
			"l.Lock calls through an interface, so it counts as calling the Lock\n    method of every type the program converts to main.Locker."}},
		{"allocation", "type slowAlloc struct{}\n\n" +
			"func (slowAlloc) Alloc(size, align uintptr) unsafe.Pointer {\n\twait()\n\treturn nil\n}\n\n" +
			"func (slowAlloc) Free(p unsafe.Pointer, size, align uintptr) {}\n\n" +
			"func init() { alloc.SetDefault(slowAlloc{}) }\n\n" +
			"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\t_ = new(int)\n}\n", []string{
			"main.p:53:6: " + irqWait,
			"keyboard allocates (main.p:53), which calls allocAt",
			"which may be slowAlloc.Alloc, which calls wait",
			"(main.p:43), which may wait.",
			"a.Alloc calls through an interface, so it counts as calling the Alloc\n    method of every type the program converts to rt.Allocator."}},
		{"panic handler waits", "//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\tvar a [4]int\n\ti := 5\n\ta[i%8] = 1\n}\n", []string{
			"onPanic is the panic handler, so it must not wait\n" +
				"    A panic can happen anywhere: in an interrupt handler, or while the code\n",
			"onPanic calls wait (main.p:", "Report the panic with code that does not wait",
			`(see "Panics in freestanding programs" in the language spec)`}},
		// Allowed: a lock that does not wait, and func values that are
		// never set to anything that does.
		{"trylock", "var hook func()\n\nfunc quick() {}\n\nfunc init() { hook = quick }\n\n" +
			"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\tif mu.TryLock() {\n\t\thook()\n\t}\n}\n", nil},
		// A panic in an interrupt handler reaches the panic handler, which
		// is checked on its own.
		{"panics", "//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\tvar a [4]int\n\ti := 5\n\ta[i%8] = 1\n}\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler := goodHandler
			if c.name == "panic handler waits" {
				handler = "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\twait()\n\tfor {\n\t}\n}\n\n"
			}
			src := "package main\n\nimport (\n\t\"pluto/alloc\"\n\t\"pluto/arch/x86\"\n\t\"pluto/rt\"\n\t\"unsafe\"\n)\n\n" +
				"var _ alloc.Allocator\nvar _ unsafe.Pointer\n\n" + handler + contextDecls + c.src
			got := lowerModeError(t, src, kernel)
			if len(c.want) == 0 && got != "" {
				t.Fatalf("unexpected error:\n%s", got)
			}
			if len(c.want) > 0 && got == "" {
				t.Fatalf("compiled; want an error containing %q", c.want)
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("error does not contain %q:\n%s", w, got)
				}
			}
			if n := strings.Count(got, "so it must not"); n > 1 {
				t.Errorf("%d errors, want one:\n%s", n, got)
			}
		})
	}
}
