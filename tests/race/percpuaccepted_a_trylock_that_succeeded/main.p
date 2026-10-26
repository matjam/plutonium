package main
import "pluto/rt"

//pu:percpu
var ticks uint64

var l rt.Lock

func main() {
	if l.TryLock() {
		ticks++
		l.Unlock()
	}
}