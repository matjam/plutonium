package main

import "pluto/sync"

type Obj struct{ n int }

var (
	rw  sync.RWMutex
	obj owned[Obj]
)

func reader(done chan bool) {
	rw.RLock()
	println(obj.n)
	rw.RUnlock()
	done <- true
}

func writer(done chan bool) {
	rw.Lock()
	obj = new(Obj{n: 2}) // frees the old Obj under the write lock
	rw.Unlock()
	done <- true
}

func main() {
	obj = new(Obj{n: 1})
	done := make(chan bool)
	task reader(done)
	task reader(done)
	task writer(done)
	<-done
	<-done
	<-done
}
