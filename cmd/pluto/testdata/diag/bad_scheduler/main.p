package main

import "pluto/rt"

type half struct{}

func (half) Spawn(t *rt.Task) {}

// The type has only one of the methods.
//
//pu:scheduler
var first half

// A second binding.
//
//pu:scheduler
var second rt.Scheduler

func main() {
	done := make(chan bool, 1)
	done <- true
	<-done
}
