package sched

import (
	"slices"
	"sync"
	"time"
)

// runTasks runs a round's tasks on the workers and returns what they give,
// in task order. With more than one worker and task, each worker takes the
// next task until none is left, the longest solves (by lastDur) first.
//
// It is in a file of its own because the self-hosted compiler has a
// hand-written version that runs the tasks in order on the first worker:
// it runs one task (tools/go2pu maps runtime.NumCPU to 1, and the locks of
// its packages are no-ops), and a channel and a task statement here would
// be reached from its panic handler through func values.
func runTasks(workers []*worker, tasks []task, lastDur []time.Duration) []outcome {
	outs := make([]outcome, len(tasks))
	if len(tasks) == 1 || len(workers) == 1 {
		for i, t := range tasks {
			outs[i] = workers[0].run(t)
		}
		return outs
	}
	var wg sync.WaitGroup
	order := make([]int, len(tasks))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmpDur(lastDur[tasks[b].id], lastDur[tasks[a].id])
	})
	next := make(chan int, len(tasks))
	for _, i := range order {
		next <- i
	}
	close(next)
	for _, w := range workers[:min(len(workers), len(tasks))] {
		wg.Add(1)
		go func(w *worker) {
			defer wg.Done()
			for i := range next {
				outs[i] = w.run(tasks[i])
			}
		}(w)
	}
	wg.Wait()
	return outs
}
