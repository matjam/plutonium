package opt

import (
	"os"
	"runtime"
	"slices"
	"sync"

	"github.com/matjam/plutonium/internal/ssa"
)

// runFuncs runs each function through passes in order. A pass changes
// only the function it is given, so the functions run in parallel, the
// largest first. The self-hosted compiler's par.p is hand-written.
func runFuncs(funcs []*ssa.Func, passes []*Pass, copyOut bool) {
	n := runtime.NumCPU()
	if n > len(funcs) {
		n = len(funcs)
	}
	if n <= 1 {
		for _, f := range funcs {
			runPasses(f, passes, copyOut)
		}
		return
	}
	order := bySize(funcs)
	var mu sync.Mutex
	next := 0
	var wg sync.WaitGroup
	for k := 0; k < n; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				j := next
				next++
				mu.Unlock()
				if j >= len(order) {
					return
				}
				runPasses(funcs[order[j]], passes, copyOut)
			}
		}()
	}
	wg.Wait()
}

// copyFuncs, set by the PLUTO_SSACOPY environment variable, copies each
// function after its passes, as the self-hosted compiler does to free
// what they dropped (ssa.Copier), to test the copy. runFuncs is told not
// to copy when something holds on to the functions' values.
var copyFuncs = os.Getenv("PLUTO_SSACOPY") != ""

// runPasses runs f through passes, and copies it if copyOut and
// copyFuncs.
func runPasses(f *ssa.Func, passes []*Pass, copyOut bool) {
	for _, ps := range passes {
		ps.Run(f)
	}
	if copyOut && copyFuncs {
		ssa.NewCopier(f).Copy(f)
	}
}

// bySize returns the indexes of funcs, the functions with the most values
// first, so that the longest ones do not start last.
func bySize(funcs []*ssa.Func) []int {
	size := make([]int, len(funcs))
	order := make([]int, len(funcs))
	for i, f := range funcs {
		order[i] = i
		for _, b := range f.Blocks {
			size[i] += len(b.Values)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int { return size[b] - size[a] })
	return order
}
