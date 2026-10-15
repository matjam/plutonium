package build_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// A caller's static closure region finalizes captures in layout order.
// A later capture's destructor cannot revive an already-released capture
// through another closure over the same region (section 15.4).
func TestNormalShutdownFinalizedRegionCapture(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/alloc"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("drop",p.id)}}
type reviver owner struct{live bool}
func(p *reviver)Drop(){if p.live{println("revive");revive();p.live=false}}
func makeItem(id int)synced[item]{p:=alloc.Synced[item]();p.id=id;return p}
func readers()(func()int,func()){
 x:=makeItem(1)
 y:=alloc.Synced[reviver]()
 y.live=true
 return func()int{if y.live{return x.id};return 0},func(){x=makeItem(2);println("unexpected committed")}
}
var read,revive=readers()
func main(){println("read",read())}
`
	for _, opt := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runNormalShutdown(t, source, opt)
			const want = "read 1\ndrop 1\nrevive\npanic: write to a finalized package resource during shutdown\n"
			if status != 2 || !strings.HasPrefix(out, want) || strings.Contains(out, "unexpected committed") {
				t.Fatalf("status %d, output %q; want prefix %q and no committed write", status, out, want)
			}
		})
	}
}
