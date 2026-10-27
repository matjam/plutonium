// Ported from internal/build/safe_arena_defer_test.go at the go-cutover tag.

package tests

import (
	"fmt"
	"testing"
)

// A produced owner receiver transfers into its armed deferred call. It must
// not be dropped at the defer statement, including when Goexit checks are active.
func TestDeferredAllocatorRestore(t *testing.T) {
	t.Parallel()
	const source = `package main
import("pluto/alloc";"pluto/arena")
func direct(a *arena.Fixed){r:=alloc.Use(a);println("direct",alloc.Default()==alloc.Allocator(a));r.Restore()}
func deferred(a *arena.Fixed){defer alloc.Use(a).Restore();println("defer",alloc.Default()==alloc.Allocator(a))}
func main(){a:=arena.NewFixed(128);direct(&a);deferred(&a);println("restored",alloc.Default()!=alloc.Allocator(&a))}
`
	for _, opt := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", opt), func(t *testing.T) {
			t.Parallel()
			out, status := runSrc(t, source, opt, "--gencheck")
			if want := "direct true\ndefer true\nrestored true\n"; status != 0 || out != want {
				t.Fatalf("status %d, output %q; want %q", status, out, want)
			}
		})
	}
}
