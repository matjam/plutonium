package main

import "pluto/sync"

type Limits struct{ max int }

var (
	limOnce sync.Once
	lim     owned[Limits]
)

func maxConns() int {
	limOnce.Do(func() { lim = new(Limits{max: 64}) })
	return lim.max
}

func reload() {
	lim = new(Limits{max: 128})
}

func main() {
	task reload()
	println(maxConns())
}
