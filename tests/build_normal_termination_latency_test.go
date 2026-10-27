// Ported from internal/build/normal_termination_latency_test.go at the go-cutover tag.

package tests

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// With more runnable tasks than processors the program still ends at once
// when main returns: the wait for tasks that are finishing is a short
// wall-clock bound, not a count of yields, which each wait a time slice when
// the machine is oversubscribed (decision D31, section 15.4).
func TestNormalTerminationOversubscribedExit(t *testing.T) {
	t.Parallel()
	const source = `package main
func spin() { for { } }
func main() {
	for i := 0; i < 64; i++ {
		task spin()
	}
	println("main returns")
}
`
	prog := mustBuild(t, single(source), "-O2", "--gencheck")
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		start := time.Now()
		out, err := exec.CommandContext(ctx, prog).CombinedOutput()
		elapsed := time.Since(start)
		cancel()
		if err != nil || string(out) != "main returns\n" {
			t.Fatalf("run %d: %v, output %q", i, err, out)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("run %d: the program took %v to end after main returned", i, elapsed)
		}
	}
}

// Tasks that release a WaitGroup with defer are still returning when Wait
// does; the short wait before the abandon path lets most of them finish, so
// package cleanup runs in practice (it is not guaranteed: decision D31).
// This test only checks that a joined program with many such tasks still
// ends with status 0 and does not wait for anything beyond the bound.
func TestNormalTerminationDeferredDoneExit(t *testing.T) {
	t.Parallel()
	const source = `package main
import "pluto/sync"
var wg sync.WaitGroup
func work() {
	defer wg.Done()
	k := 0
	for j := 0; j < 20000; j++ {
		k += j
	}
	_ = k
}
func main() {
	for i := 0; i < 500; i++ {
		wg.Add(1)
		task work()
	}
	wg.Wait()
	println("joined")
}
`
	prog := mustBuild(t, single(source), "-O2", "--gencheck")
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		start := time.Now()
		out, err := exec.CommandContext(ctx, prog).CombinedOutput()
		elapsed := time.Since(start)
		cancel()
		if err != nil || string(out) != "joined\n" {
			t.Fatalf("run %d: %v, output %q", i, err, out)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("run %d: the program took %v", i, elapsed)
		}
	}
}
