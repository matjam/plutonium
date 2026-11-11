package main

import (
	"pluto/rt"
	"pluto/sync"
)

var (
	once sync.Once
	cfg  int
	flag bool
)

func bad() bool {
	return flag
}

func load() {
	cfg = 1
	if bad() {
		rt.ExitTask()
	}
	cfg = 2
}

func show() {
	println(cfg)
}

func worker() {
	defer show()
	once.Do(load)
}

func main() {
	task worker()
	task worker()
}
