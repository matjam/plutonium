// Concurrency micro-benchmarks, the Plutonium version of ../main.go:
// channel ping-pong, buffered channel throughput, puroutine start cost,
// and mutex throughput uncontended and under contention. Each prints
// nanoseconds per operation.
package main

import (
	"pluto/sync"
	"pluto/syscall"
	"unsafe"
)

type timespec struct{ sec, nsec int64 }

func now() int64 {
	var ts timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, 1, uintptr(unsafe.Pointer(&ts)), 0)
	return ts.sec*1000000000 + ts.nsec
}

func pong(in, out chan int) {
	for v := range in {
		out <- v + 1
	}
}

func pingpong(n int) int64 {
	a := make(chan int)
	b := make(chan int)
	go pong(a, b)
	start := now()
	v := 0
	for i := 0; i < n; i++ {
		a <- v
		v = <-b
	}
	elapsed := now() - start
	close(a)
	return elapsed / int64(n)
}

func drain(in chan int, done chan int) {
	s := 0
	for v := range in {
		s += v
	}
	done <- s
}

func buffered(n int) int64 {
	c := make(chan int, 1024)
	done := make(chan int)
	go drain(c, done)
	start := now()
	for i := 0; i < n; i++ {
		c <- i
	}
	close(c)
	<-done
	elapsed := now() - start
	return elapsed / int64(n)
}

var wg sync.WaitGroup

func nothing() { wg.Done() }

func spawn(n int) int64 {
	start := now()
	for i := 0; i < n; i += 1000 {
		wg.Add(1000)
		for j := 0; j < 1000; j++ {
			go nothing()
		}
		wg.Wait()
	}
	return (now() - start) / int64(n)
}

var (
	mu      sync.Mutex
	counter int
	mwg     sync.WaitGroup
)

func locker(n int) {
	for i := 0; i < n; i++ {
		mu.Lock()
		counter++
		mu.Unlock()
	}
	mwg.Done()
}

func mutex(workers, n int) int64 {
	start := now()
	mwg.Add(workers)
	for i := 0; i < workers; i++ {
		go locker(n)
	}
	mwg.Wait()
	return (now() - start) / int64(workers*n)
}

func main() {
	println("pingpong", pingpong(1000000), "ns/op")
	println("buffered", buffered(10000000), "ns/op")
	println("spawn", spawn(1000000), "ns/op")
	println("mutex-1", mutex(1, 10000000), "ns/op")
	println("mutex-8", mutex(8, 1000000), "ns/op")
}
