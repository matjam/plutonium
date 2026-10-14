package build_test

import (
	"fmt"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// A produced owner receiver transfers into its armed deferred call. It must
// not be dropped at the defer statement, including when Goexit checks are active.
func TestDeferredAllocatorRestore(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import("pluto/alloc";"pluto/arena")
func direct(a *arena.Fixed){r:=alloc.Use(a);println("direct",alloc.Default()==alloc.Allocator(a));r.Restore()}
func deferred(a *arena.Fixed){defer alloc.Use(a).Restore();println("defer",alloc.Default()==alloc.Allocator(a))}
func main(){a:=arena.NewFixed(128);direct(&a);deferred(&a);println("restored",alloc.Default()!=alloc.Allocator(&a))}
`
	for _, opt := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runGenCheck(t, source, opt)
			if want := "direct true\ndefer true\nrestored true\n"; status != 0 || out != want {
				t.Fatalf("status %d, output %q; want %q", status, out, want)
			}
		})
	}
}
