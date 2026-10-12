package build_test

import (
	"fmt"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestGenerationDirectoryConcurrentReuse exercises metadata snapshots while
// another task reuses records with different origins and bounds. The payload
// read by main is immutable, separately registered, and remains live until
// the worker finishes. No read races with a payload write or reclamation.
func TestGenerationDirectoryConcurrentReuse(t *testing.T) {
	testguard.Parallel(t)
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runGenCheck(t, generationDirectoryConcurrentSource, level)
			if status != 0 || out != "concurrent metadata ok\n" {
				t.Fatalf("want successful concurrent metadata check: status %d, output %q", status, out)
			}
		})
	}
}

const generationDirectoryConcurrentSource = `package main
import ("pluto/rt"; "unsafe")

func churn(base uintptr, started, done chan int) {
 started <- 1
 for i := 0; i < 6000; i++ {
  // Both extents fit the same record capacity, but their origins and
  // bounds differ. The first shares the reader's 64 KiB interval.
  off, size := uintptr(256), uintptr(80)
  if i & 1 != 0 { off, size = 65536+4096, 112 }
  p := unsafe.Pointer(base+off)
  token := rt.GenRegister(p, size, nil, 0)
  live := rt.GenTagHeap(token, p, size)
  if live == nil || !rt.GenRegisteredLive(live) { panic("churn registration lost") }
  rt.GenUnregister(token)
 }
 done <- 1
}

func main() {
 raw := rt.Mmap(196608)
 if raw == nil { panic("mmap failed") }
 defer rt.Munmap(raw, 196608)
 base := (uintptr(raw)+65535) &^ uintptr(65535)
 // Between the two disjoint churn extents: a torn old-lo/new-hi snapshot
 // could accept this address and underflow against the newer origin.
 stableRaw := unsafe.Pointer(base+4096)
 stableToken := rt.GenRegister(stableRaw, 16, nil, 0)
 stable := rt.GenTagHeap(stableToken, stableRaw, 16)
 *(*byte)(stable) = 73
 started, done := make(chan int), make(chan int)
 task churn(base, started, done)
 <-started
 for i := 0; i < 200000; i++ {
  if !rt.GenRegisteredLive(stable) || *(*byte)(stable) != 73 {
   panic("stable registration lost during unrelated reuse")
  }
 }
 <-done
 if !rt.GenRegisteredLive(stable) { panic("stable registration lost after reuse") }
 rt.GenUnregister(stableToken)
 if rt.GenRegisteredLive(stable) { panic("stable registration survived unregister") }
 println("concurrent metadata ok")
}
`
