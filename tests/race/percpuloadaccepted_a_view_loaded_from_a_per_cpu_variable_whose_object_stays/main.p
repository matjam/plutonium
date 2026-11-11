package main

import "pluto/rt"

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

type Node struct{ n int }

//pu:percpu
var cur *Node

var b owned[Node]

func reader() {
	p := cur
	println(p.n)
}

func main() {
	b = new(Node)
	cur = &*b
	task reader()
	rt.Yield()
}
