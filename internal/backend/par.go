package backend

import (
	"runtime"
	"sync"

	"github.com/matjam/plutonium/internal/arch/amd64"
	"github.com/matjam/plutonium/internal/scratch"
	"github.com/matjam/plutonium/internal/ssa"
)

// emitAll generates the code of funcs and adds it to the object in order.
// Code generation changes only the function it is given, so batches of
// functions are compiled in parallel and then added one by one. The
// self-hosted compiler's par.p is hand-written.
func (e *emitter) emitAll(funcs []*ssa.Func) error {
	n := min(runtime.NumCPU(), len(funcs))
	if n <= 1 || e.cfg.Listing != nil {
		for _, f := range funcs {
			e.f = f
			scratch.Do(e)
			if e.err != nil {
				return e.err
			}
		}
		return nil
	}
	batch := 4 * n
	codes := make([]*amd64.Code, batch)
	errs := make([]error, batch)
	for lo := 0; lo < len(funcs); lo += batch {
		part := funcs[lo:min(lo+batch, len(funcs))]
		var mu sync.Mutex
		next := 0
		var wg sync.WaitGroup
		for k := 0; k < min(n, len(part)); k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					mu.Lock()
					i := next
					next++
					mu.Unlock()
					if i >= len(part) {
						return
					}
					codes[i], errs[i] = compile(part[i])
				}
			}()
		}
		wg.Wait()
		for i, f := range part {
			if errs[i] != nil {
				return errs[i]
			}
			if err := e.emit(f, codes[i]); err != nil {
				return err
			}
			codes[i] = nil
		}
	}
	return nil
}

// compile generates f's code, as emitter.ScratchWork does.
func compile(f *ssa.Func) (*amd64.Code, error) {
	amd64.Prepare(f)
	if err := f.Verify(); err != nil {
		return nil, err
	}
	return amd64.Compile(f, false)
}
