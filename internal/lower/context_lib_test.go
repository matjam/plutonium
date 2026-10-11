package lower_test

import (
	"strings"
	"testing"
)

// TestInterruptContextLibrary checks the interrupt-context check through
// the standard library, where nothing but the runtime's park and spawn is
// marked: the check follows the library's calls to them, and the message
// names the path only as far as the call into the library.
func TestInterruptContextLibrary(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string // fragments of the error; none if it must compile
		notWant    []string
	}{
		{"mutex", "mu.Lock()", []string{
			"keyboard is an interrupt handler, so it must not wait",
			"keyboard waits on mu (main.p:28).",
			`(run "pluto explain E2584"`}, []string{"lockSlow", "park"}},
		{"through own code", "readConfig()", []string{
			"keyboard calls readConfig (main.p:28), which waits on mu (main.p:19)."}, []string{"acquire"}},
		{"park until", "rt.ParkUntil(rt.Now() + 1)", []string{
			"keyboard calls rt.ParkUntil (main.p:28), which may wait."}, []string{"park ("}},
		{"wait group go", "wg.Task(work)", []string{
			"keyboard is an interrupt handler, so it must not start a task",
			"keyboard calls wg.Task (main.p:28), which starts a task.",
			`(run "pluto explain E2585"`}, []string{"spawn"}},
		{"rt go", "rt.Go(work, rt.MinStackSize)", []string{
			"keyboard calls rt.Go (main.p:28), which starts a task."}, nil},
		{"channel", "ch <- 1", []string{"keyboard sends on ch (main.p:28)."}, nil},

		{"trylock and unlock", "if mu.TryLock() {\n\t\tmu.Unlock()\n\t}", nil, nil},
		{"wait group done", "wg.Done()", nil, nil},
		{"non-blocking send", "select {\n\tcase ch <- 1:\n\tdefault:\n\t}", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\n\nimport (\n\t\"pluto/arch/x86\"\n\t\"pluto/rt\"\n\t\"pluto/sync\"\n\t\"pluto/time\"\n)\n\n" +
				"var (\n\tmu sync.Mutex\n\twg sync.WaitGroup\n\tch = make(chan int, 1)\n)\n\n" +
				"func work() {}\n\nfunc readConfig() {\n\tmu.Lock()\n\tmu.Unlock()\n}\n\n" +
				"var _ = rt.MinStackSize\nvar _ = time.Second\n\n" +
				"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n\t" + c.body + "\n}\n\n" +
				"func main() { _ = x86.HandlerAddr(keyboard) }\n"
			got := lowerModeError(t, src, hosted)
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
			for _, w := range c.notWant {
				if strings.Contains(got, w) {
					t.Errorf("error contains %q:\n%s", w, got)
				}
			}
		})
	}
}
