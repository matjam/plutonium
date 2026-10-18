package lower_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// TestInferNoReturn checks that the compiler works out which functions
// never return, the library's and the program's own, with no directive:
// every call of one ends its block, and calls of functions that may return
// do not.
func TestInferNoReturn(t *testing.T) {
	prog := lowerSource(t, `package main

import (
	"pluto/os"
	"pluto/os/exec"
	"pluto/rt"
	"pluto/sync"
	"pluto/syscall"
)

type fataler struct{ code int }

func (f *fataler) fail() { os.Exit(f.code) }

func die(msg string) { panic(msg) }

func spin() {
	for {
	}
}

func labeled(n int) {
outer:
	for {
		for {
			if n > 0 {
				break outer
			}
		}
	}
}

func either(n int) {
	if n > 0 {
		die("positive")
	} else {
		os.Exit(1)
	}
}

func switched(n int) {
	switch n {
	case 0:
		spin()
	default:
		die("other")
	}
}

func noDefault(n int) {
	switch n {
	case 0:
		spin()
	}
}

func returns(n int) {
	for {
		if n > 0 {
			return
		}
	}
}

func generic[T any](x T) { die("generic") }

func recurse(n int) { recurse(n + 1) }

func main() {
	var f fataler
	var mu sync.Mutex
	mu.Lock()
	mu.Unlock()
	c := exec.Command("true")
	_ = c.Run()
	n := len(os.GetenvLossy("N"))
	if n > 5 {
		f.fail()
	}
	if n > 6 {
		either(1)
	}
	if n > 7 {
		switched(1)
	}
	if n > 8 {
		generic(1)
	}
	if n > 9 {
		spin()
	}
	if n > 10 {
		syscall.Exit(1)
	}
	if n > 11 {
		rt.Abort()
	}
	labeled(1)
	noDefault(1)
	returns(1)
	recurse(1)
	os.Exit(0)
}
`)
	never := map[string]bool{
		"main.(*fataler).fail": true, "main.die": true, "main.spin": true, "main.either": true,
		"main.switched": true, "main.generic[int]": true,
		"pluto/os.Exit": true, "pluto/syscall.Exit": true, "pluto/rt.Exit": true, "pluto/rt.exit": true,
		"pluto/rt.Abort": true, "pluto/rt.fatal": true, "pluto/rt.raise": true, "pluto/rt.lastResort": true,
		"pluto/sync.fail": true, "pluto/os/exec.childFail": true, "pluto/os/exec.child": true,
		"pluto/rt.defaultPanic": true,
	}
	returning := map[string]bool{
		"main.labeled": true, "main.noDefault": true, "main.returns": true, "main.recurse": true,
	}
	seen := map[string]bool{}
	for _, f := range prog.Funcs {
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				callee, ok := v.Aux.(*ssa.Sym)
				if v.Op != ssa.OpCall || !ok {
					continue
				}
				nr := v.Call != nil && v.Call.NoReturn
				switch {
				case never[callee.Name]:
					seen[callee.Name] = true
					if !nr {
						t.Errorf("%s: call of %s may return", f.Name, callee.Name)
					}
					if b.Kind != ssa.BlockExit {
						t.Errorf("%s: call of %s does not end its block", f.Name, callee.Name)
					}
				case returning[callee.Name]:
					seen[callee.Name] = true
					if nr {
						t.Errorf("%s: call of %s never returns", f.Name, callee.Name)
					}
				}
			}
		}
	}
	var missing []string
	for name := range never {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	for name := range returning {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no calls of %s", strings.Join(missing, ", "))
	}
}
