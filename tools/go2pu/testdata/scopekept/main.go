// Command scopekept holds a sync.WaitGroup in each form go2pu does not
// rewrite to a sync.Scope, for TestScopeKept. It is never translated into
// the build.
package main

import (
	"fmt"
	"sync"
)

var sink []int

func keepAddr(p *int) { sink = append(sink, *p) }

func finish(wg *sync.WaitGroup) { wg.Done() }

// latch: the goroutine calls Done and keeps running.
func latch() {
	var wg sync.WaitGroup
	ready := 0
	wg.Add(1)
	go func() {
		ready = 1
		wg.Done()
		sink = append(sink, ready)
	}()
	wg.Wait()
}

// passed: the WaitGroup is passed to another function.
func passed() {
	var wg sync.WaitGroup
	n := 0
	wg.Add(1)
	go func() {
		n++
		finish(&wg)
	}()
	wg.Wait()
	fmt.Println(n)
}

// reassigned: the argument changes after the go statement.
func reassigned() {
	var wg sync.WaitGroup
	k := 1
	wg.Add(1)
	go func(v int) {
		defer wg.Done()
		sink = append(sink, v)
	}(k)
	k = 2
	wg.Wait()
	fmt.Println(k)
}

// early: the goroutine can return before it calls Done.
func early(stop bool) {
	var wg sync.WaitGroup
	n := 0
	wg.Add(1)
	go func() {
		if stop {
			return
		}
		n++
		wg.Done()
	}()
	wg.Wait()
}

// lateDefer: Done is deferred after another statement.
func lateDefer() {
	var wg sync.WaitGroup
	n := 0
	wg.Add(1)
	go func() {
		n++
		defer wg.Done()
	}()
	wg.Wait()
}

// noAdd: the goroutine is not preceded by an Add.
func noAdd() {
	var wg sync.WaitGroup
	n := 0
	go func() {
		defer wg.Done()
		n++
	}()
	wg.Wait()
}

// noWait: nothing waits for the goroutines.
func noWait() {
	var wg sync.WaitGroup
	n := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		n++
	}()
}

// nested: the goroutine is started in a function literal.
func nested() {
	var wg sync.WaitGroup
	n := 0
	wg.Add(1)
	func() {
		go func() {
			defer wg.Done()
			n++
		}()
	}()
	wg.Wait()
}

// pointer: the WaitGroup is allocated.
func pointer() {
	wg := &sync.WaitGroup{}
	n := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		n++
	}()
	wg.Wait()
}

// strayAdd: an Add without a goroutine.
func strayAdd() {
	var wg sync.WaitGroup
	wg.Add(2)
	wg.Add(1)
	go func() {
		defer wg.Done()
	}()
	wg.Wait()
}

// namedResult: the argument is a result that return assigns.
func namedResult() (r int) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func(v int) {
		defer wg.Done()
		sink = append(sink, v)
	}(r)
	wg.Wait()
	return 5
}

// shadowed: the argument's name means something else where the goroutine
// uses its parameter.
func shadowed() {
	var wg sync.WaitGroup
	var out [2]int
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			i := 7
			out[id] = i
		}(i)
	}
	wg.Wait()
}

// mixed rewrites its WaitGroup. The variable that a function keeps the
// address of stays on the heap, and the one only the task uses stays in
// the frame.
func mixed() int {
	var wg sync.WaitGroup
	shared := 0
	total := 0
	keepAddr(&shared)
	wg.Go(func() {
		shared++
		total += 2
	})
	wg.Wait()
	return shared + total
}

func main() {
	latch()
	passed()
	reassigned()
	early(false)
	lateDefer()
	noAdd()
	noWait()
	nested()
	pointer()
	strayAdd()
	namedResult()
	shadowed()
	fmt.Println(mixed())
}
