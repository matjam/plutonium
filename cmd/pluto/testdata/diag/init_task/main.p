package main

import (
	"pluto/sync"
	"pluto/time"
)

var value = start()
var _, second = pair()
var third = makeOwner().value
var _ = makeOwner()

type holder owner struct {
	active bool
	value  int
}

func (h *holder) Drop() {
	if h.active {
		task worker()
	}
}

func makeOwner() owned[holder] {
	return new(holder{active: true, value: 3})
}

func start() int {
	task worker()
	return 1
}

func pair() (int, int) {
	start()
	return 1, 2
}

func init() {
	time.Sleep(1)
	var scope sync.Scope
	scope.Task(worker)
}

func worker() {}

func main() {}
