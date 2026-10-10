package main

import (
	"pluto/rt"
	"pluto/time"
)

// A program that binds its own scheduler, which has no clock or timer,
// and sleeps.
type noClock struct{}

func (noClock) NewTask(req rt.StackRequest) *rt.Task { return nil }
func (noClock) FreeTask(t *rt.Task)                  {}
func (noClock) Spawn(t *rt.Task)                     {}
func (noClock) Park(t *rt.Task)                      {}
func (noClock) Wake(t *rt.Task)                      {}
func (noClock) Yield(t *rt.Task)                     {}

//pu:scheduler
var sched noClock

func main() {
	time.Sleep(time.Millisecond)
}
