package build_test

import (
	"strconv"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// A shared holder preserves the Timer's address while both tasks use its
// internally synchronized Reset and Stop operations (section 19.6).
func TestNormalShutdownConcurrentTimer(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import (
 "pluto/alloc"
 "pluto/time"
)
type holder owner struct{ timer owned[time.Timer] }
var kept synced[holder]
func fire(){}
func worker(h synced[holder], done chan bool){
 for i:=0;i<128;i++{h.timer.Reset(time.Hour);h.timer.Stop()}
 done<-true
}
func makeHolder()synced[holder]{
 h:=alloc.Synced[holder]()
 h.timer=time.AfterFunc(time.Hour,fire)
 return h
}
func main(){
 kept=makeHolder()
 done:=make(chan bool,2)
 task worker(kept,done)
 task worker(kept,done)
 <-done; <-done
 kept.timer.Stop()
 println("done")
}
`
	for _, opt := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runNormalShutdown(t, source, opt)
			if status != 0 || out != "done\n" {
				t.Fatalf("status %d, output %q; want status 0 and done", status, out)
			}
		})
	}
}
