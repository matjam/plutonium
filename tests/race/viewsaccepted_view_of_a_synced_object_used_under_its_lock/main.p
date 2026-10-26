package main
import (
	"pluto/sync"
	"pluto/vec"
)

type DB owner struct {
	mu   sync.Mutex
	rows vec.Vec[int]
}

func reader(d synced[DB], done chan bool) {
	d.mu.Lock()
	p := d.rows.Ptr(0)
	println(*p)
	d.mu.Unlock()
	done <- true
}

func writer(d synced[DB], done chan bool) {
	d.mu.Lock()
	for i := 0; i < 100; i++ {
		d.rows.Push(i)
	}
	d.mu.Unlock()
	done <- true
}

func main() {
	var d synced[DB]
	d.rows.Push(1)
	done := make(chan bool)
	task reader(d, done)
	task writer(d, done)
	<-done
	<-done
}