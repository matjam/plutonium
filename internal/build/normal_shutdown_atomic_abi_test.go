package build_test

import (
	"strconv"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// With no resource-bearing globals, removing shutdown checks must retain
// the external ABI's pointer untagging, including through function values.
func TestNormalShutdownAtomicABI(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/alloc"
import "pluto/sync/atomic"
var store=atomic.StoreInt32
var swap=atomic.SwapInt32
var add=atomic.AddInt32
func main(){
 p:=alloc.Synced[int32]()
 atomic.StoreInt32(&*p,5)
 println("direct",atomic.LoadInt32(&*p))
 saved:=store
 saved(&*p,7)
 println("saved",swap(&*p,9),add(&*p,3),atomic.LoadInt32(&*p))
}
`
	const want = "direct 5\nsaved 7 12 12\n"
	for _, opt := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runNormalShutdown(t, source, opt)
			if status != 0 || out != want {
				t.Fatalf("status %d, output %q; want %q", status, out, want)
			}
		})
	}
}
