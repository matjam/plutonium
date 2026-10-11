// Command scopes holds the forms in which go2pu rewrites a local
// sync.WaitGroup to a sync.Scope (scope.go). Its goroutines use the
// variables of the function that starts them, which Plutonium allows for a
// Scope but not for a WaitGroup. TestScopeRewrites checks the translation,
// and TestDifferential checks that Go and Plutonium print the same text.
package main

import (
	"fmt"
	"sync"
)

const n = 8

func work(x int) int { return x*x + 1 }

// deferred: Add before the loop's go statement, defer wg.Done() first.
func deferred() [n]int {
	var wg sync.WaitGroup
	var out [n]int
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = work(i)
		}()
	}
	wg.Wait()
	return out
}

// atEnd: Done is the last statement, and one Add covers the whole loop.
func atEnd() [n]int {
	wg := sync.WaitGroup{}
	var out [n]int
	wg.Add(n)
	for i := range n {
		go func() {
			out[i] = work(i) + 1
			wg.Done()
		}()
	}
	wg.Wait()
	return out
}

// args: the literal's parameters stand for the loop variable, whose value
// the go statement passed.
func args() [n]int {
	var wg sync.WaitGroup
	var out [n]int
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			out[k] = 3 * k
		}(i)
	}
	wg.Wait()
	return out
}

// renamed: the parameter has another name than its argument.
func renamed() [n]int {
	var wg sync.WaitGroup
	var out [n]int
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			out[id] = id + 100
			wg.Done()
		}(i)
	}
	wg.Wait()
	return out
}

// goForm: wg.Go with workers that share a counter under a mutex.
func goForm() int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for w := 1; w <= 4; w++ {
		wg.Go(func() {
			for k := 0; k < 100; k++ {
				mu.Lock()
				total += w
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return total
}

// twice: two WaitGroups in one function, and the results read between the
// two waits.
func twice() (int, int) {
	var first sync.WaitGroup
	var second sync.WaitGroup
	var a, b [4]int
	for i := 0; i < 4; i++ {
		first.Add(1)
		go func() {
			defer first.Done()
			a[i] = i + 1
		}()
	}
	first.Wait()
	sum := 0
	for _, v := range a {
		sum += v
	}
	for i := 0; i < 4; i++ {
		second.Add(1)
		go func() {
			defer second.Done()
			b[i] = a[i] * 2
		}()
	}
	second.Wait()
	return sum, b[3]
}

func main() {
	fmt.Println(deferred())
	fmt.Println(atEnd())
	fmt.Println(args())
	fmt.Println(renamed())
	fmt.Println(goForm())
	fmt.Println(twice())
}
