package lower_test

import (
	"strings"
	"testing"
)

// Catching cleanup Goexit does not suppress a real allocator callback's wait
// effect. The named runtime contract changes exit propagation only.
func TestArenaCleanupPreservesWaitEffect(t *testing.T) {
	src := `package main
import ("pluto/rt";"unsafe")
type waiting struct{}
func(*waiting)Alloc(size,align uintptr)unsafe.Pointer{return nil}
func(*waiting)Free(p unsafe.Pointer,size,align uintptr){rt.Park()}
var parent waiting
//pu:panic
func onPanic(p *rt.Panic){rt.CatchAllocFree(&parent,nil,0,1);rt.Abort()}
func main(){}
`
	err := lowerModeError(t, src, hosted)
	if !strings.Contains(err, "onPanic is the panic handler, so it must not wait") || !strings.Contains(err, "may wait") {
		t.Fatalf("real callback wait effect was lost:\n%s", err)
	}
}
