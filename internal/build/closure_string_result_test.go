package build_test

import (
	"os"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Counted results survive closure region cleanup (sections 11.5 and 18.8).
func TestClosureStringResultLeaks(t *testing.T) {
	testguard.Parallel(t)
	src, err := os.ReadFile("../../testdata/run/closurestringresult.p")
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			exe := buildDebugAlloc(t, string(src), level)
			got, status := runDebugAlloc(t, exe, "leaks=fail")
			const want = "hello world\nhello again\nhello again\nhello again\n"
			if status != 0 || got != want {
				t.Fatalf("exit %d, output %q; want %q", status, got, want)
			}
		})
	}
}

// The temporary lives through its statement; a copied strong field owns
// an independent reference (sections 18.3, 18.6 and 18.7).
func TestTemporaryBoxImmediateViews(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main
import "pluto/alloc"
type Item struct { x int }
type Holder struct { p synced[Item] }
type SharedHolder struct { p shared[Item] }
func mk() owned[Item] { return new(Item{x: 7}) }
func holder() Holder { p := alloc.Synced[Item](); p.x = 11; return Holder{p} }
func sharedHolder() SharedHolder { p := alloc.Shared[Item](); p.x = 13; return SharedHolder{p} }
func read(p *int) int { return *p }
func check() {
 println(read(&mk().x))
 if p := &mk().x; *p > 0 { println(*p) }
 switch p := &mk().x; *p { case 7: println(*p) }
 q := holder().p
 p := &q.x
 println(*p)
 s := sharedHolder().p
 r := &s.x
 println(*r)
}
func main() { check(); println(alloc.ReportLeaks()) }
`
	for _, level := range []int{0, 2} {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			exe := buildDebugAlloc(t, src, level)
			got, status := runDebugAlloc(t, exe, "leaks=fail")
			const want = "7\n7\n7\n11\n13\n0\n"
			if status != 0 || got != want {
				t.Fatalf("exit %d, output %q; want %q", status, got, want)
			}
		})
	}
}
