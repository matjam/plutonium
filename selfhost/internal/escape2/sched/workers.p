package sched

import "selfhost/compat"

// runTasks is the Go compiler's runTasks (internal/escape2/sched/
// workers.go) for a compiler that runs one task: the tasks run in order on
// the first worker. Their outcomes do not depend on the order (parallel.go),
// so this gives what the Go version gives with any number of workers.
// Hand-written; go2pu keeps it in place of a translation of workers.go,
// whose channel and task statement the panic handler would reach through
// func values (lower/context.go).
func runTasks(workers []*worker, tasks []task, lastDur []compat.Duration) []outcome {
	_ = lastDur
	outs := compat.Make[outcome](len(tasks))
	for i, t := range tasks {
		outs[i] = workers[0].run(t)
	}
	return outs
}
