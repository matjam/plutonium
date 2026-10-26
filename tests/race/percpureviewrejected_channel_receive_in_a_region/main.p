package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

type ks struct{}

//pu:scheduler
var sched ks

func (ks) Init()                                {}
func (ks) NewTask(req rt.StackRequest) *rt.Task { return nil }
func (ks) FreeTask(t *rt.Task)                  {}
func (ks) Spawn(t *rt.Task)                     {}
func (ks) Wake(t *rt.Task)                      {}
func (ks) Park(t *rt.Task)                      {}
func (ks) Yield(t *rt.Task)                     {}
func (ks) InInterrupt() bool                    { return false }
func (ks) Now() int64                           { return 0 }
func (ks) ArmTimer(deadline int64)              {}

func main() {
	ch := make(chan int)
	rt.DisablePreemption()
	x := ticks
	<-ch
	ticks = x + 1
	rt.EnablePreemption()
}