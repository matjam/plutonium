package main

import "pluto/rt"

// ks is the scheduler a kernel build that starts tasks or uses channels
// needs. It does nothing: the case is only compiled. The case's program
// is in main.p.
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
