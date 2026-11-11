package main

import "pluto/sync"

var (
	once sync.Once
	cfg  int
)

func load() {
	cfg = 42
}

func worker() {
	once.Do(load)
}

func main() {
	task worker()
	println(cfg)
}
