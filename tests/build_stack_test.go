// Ported from internal/build/stack_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// TestMainStackOverflow checks that running off the end of a stack reserved
// with //pu:stack lands in its guard region and is reported as a stack
// overflow, like an overflow of the system's stack.
func TestMainStackOverflow(t *testing.T) {
	t.Parallel()
	const src = `package main

//pu:noinline
func down(n int) int {
	var pad [64]int
	pad[n%64] = n
	return down(n+1) + pad[0]
}

//pu:stack 1M
func main() {
	println(down(0))
}
`
	for _, level := range []int{0, 2} {
		out, status := runSrc(t, src, level)
		want := "panic: runtime error: stack overflow (main's stack is 1 MiB; ask for a larger one with //pu:stack on func main)\n"
		if status != 2 || !strings.HasPrefix(out, want) {
			t.Errorf("-O%d: exit status %d, output starts %q; want 2 and a stack overflow", level, status, firstLineOf(out))
		}
	}
}

// TestLargeFrameOverflow checks that a function whose 2 MiB frame is
// larger than the guard region below its stack (one page below a stack
// from rt.Go, 1 MiB below a hosted task's or a //pu:stack one) probes its
// frame, so that running out of stack faults in the guard and is reported
// as a stack overflow that names the stack's size and how to ask for more
// (language spec sections 17.5 and 19.1), rather than writing below the
// guard into other memory.
func TestLargeFrameOverflow(t *testing.T) {
	t.Parallel()
	const big = `
// big has a 2 MiB frame.
//
//pu:noinline
func big(n int) int {
	var buf [2 << 20]byte
	buf[n&1023] = byte(n)
	if n > 1000 {
		return int(buf[(n+1)&1023])
	}
	return big(n+1) + int(buf[(n+1)&1023])
}
`
	cases := []struct{ name, src, want string }{
		{"rt.Go", `package main

import "pluto/rt"
` + big + `
func run() { println(big(1000)) }

func main() {
	done := make(chan int)
	rt.Go(run, 64<<10)
	<-done
}
`, "panic: runtime error: stack overflow (the task's stack is 72 KiB; start the task with rt.Go(f, size) for a larger one)"},
		{"task statement", `package main
` + big + `
func run(done chan int) { done <- big(0) }

func main() {
	done := make(chan int)
	task run(done)
	println(<-done)
}
`, "panic: runtime error: stack overflow (the task's stack is 64 MiB; start the task with rt.Go(f, size) for a larger one)"},
		{"main", `package main
` + big + `
//pu:stack 4M
func main() { println(big(0)) }
`, "panic: runtime error: stack overflow (main's stack is 4 MiB; ask for a larger one with //pu:stack on func main)"},
	}
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			prog := mustBuild(t, single(c.src), optFlag(level))
			out, status := runExeEnv(t, prog)
			lines := strings.Split(out, "\n")
			if status != 2 || len(lines) < 3 || lines[0] != c.want || !strings.HasPrefix(lines[1], "[signal SIGSEGV") {
				t.Errorf("%s, -O%d: status %d, output:\n%s\nwant first line:\n%s", c.name, level, status, out, c.want)
			}
		}
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestMainStackErrors checks that malformed or misplaced //pu:stack
// directives stop the build.
func TestMainStackErrors(t *testing.T) {
	t.Parallel()
	// Each form has its own code (spec 20.3): E2603 for a missing or
	// malformed size, E2561 for one outside 64K to 64G, E2602 on a
	// function other than main.
	cases := []struct{ name, src, want, code string }{
		{"missing size", "package main\n\n//pu:stack\nfunc main() {}\n", "//pu:stack: missing size", "E2603"},
		{"bad size", "package main\n\n//pu:stack lots\nfunc main() {}\n", `//pu:stack: invalid size "lots"`, "E2603"},
		{"bad suffix", "package main\n\n//pu:stack 12T\nfunc main() {}\n", `invalid size "12T"`, "E2603"},
		{"too small", "package main\n\n//pu:stack 4K\nfunc main() {}\n", "smaller than 64K", "E2561"},
		{"too large", "package main\n\n//pu:stack 65G\nfunc main() {}\n", "larger than 64G", "E2561"},
		{"overflowing", "package main\n\n//pu:stack 99999999999999999G\nfunc main() {}\n", "larger than 64G", "E2561"},
		{"not main", "package main\n\n//pu:stack 1M\nfunc f() {}\n\nfunc main() { f() }\n", "//pu:stack applies only to func main", "E2602"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			errs := buildFails(t, c.src)
			if !strings.Contains(errs, c.want) || !strings.Contains(errs, "pluto explain "+c.code) {
				t.Fatalf("got errors %q, want %q with code %s", errs, c.want, c.code)
			}
		})
	}
	t.Run("freestanding", func(t *testing.T) {
		t.Parallel()
		src := "package main\n\n//pu:stack 1M\nfunc main() {}\n"
		errs := buildFails(t, src, "--mode", "freestanding", "--gencheck=false")
		if !strings.Contains(errs, "not supported in freestanding builds") || !strings.Contains(errs, "pluto explain E2560") {
			t.Fatalf("got errors %q, want the freestanding error", errs)
		}
	})
}
