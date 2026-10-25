package lower_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// TestArenaFreeNotGoexit checks that an arena passing on its parent
// allocator's exits (rt.ResumeAllocFreeExit) makes a Free call a possible
// rt.ExitTask only when some allocator's Free may end the task. Taken as a
// plain call of rt.ExitTask, it made every Free in a program that installs
// an arena check for an exit, which multiplied the self-hosted compiler's
// code tenfold. Likewise a release the compiler inserts is checked only
// when some Drop method or Free may end the task.
func TestArenaFreeNotGoexit(t *testing.T) {
	const prog = `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/rt"
	"unsafe"
)

%s

//pu:noinline
func release(a alloc.Allocator, p unsafe.Pointer) {
	a.Free(p, 8, 8)
	println("freed")
}

//pu:noinline
func concat(a, b string) {
	s := a + b
	println(s)
}

func main() {
	concat("a", "b")
	if false {
		rt.ExitTask()
	}
	a := arena.NewChunked(1<<12, alloc.Default())
	r := alloc.Use(&a)
	p := alloc.Default().Alloc(8, 8)
	r.Restore()
	release(&a, p)
}
`
	for _, c := range []struct {
		name, decls string
		want        bool
	}{
		{"arena only", "", false},
		{"exiting allocator", `type exiting struct{}

func (*exiting) Alloc(size, align uintptr) unsafe.Pointer { return nil }
func (*exiting) Free(p unsafe.Pointer, size, align uintptr) { rt.ExitTask() }

var _ alloc.Allocator = &exiting{}`, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := lowerSource(t, strings.Replace(prog, "%s", c.decls, 1))
			if got := callsGoexitCheck(p, "main.release"); got != c.want {
				t.Errorf("main.release checks for an exit after Free: %v, want %v", got, c.want)
			}
			// Releasing s may free it, which ends the task only if a Free may.
			if got := callsGoexitCheck(p, "main.concat"); got != c.want {
				t.Errorf("main.concat checks for an exit after a release: %v, want %v", got, c.want)
			}
		})
	}
}

// callsGoexitCheck reports whether the function name of p calls
// rt.takeGoexit, the check that follows a call that may end the task.
func callsGoexitCheck(p *ssa.Program, name string) bool {
	for _, f := range p.Funcs {
		if f.Name != name {
			continue
		}
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op == ssa.OpCall {
					if s, ok := v.Aux.(*ssa.Sym); ok && strings.HasSuffix(s.Name, "takeGoexit") {
						return true
					}
				}
			}
		}
	}
	return false
}
