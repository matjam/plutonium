package build_test

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// A custom backend's source method may exit before returning a task, including
// when creation runs as a deferred call. Remaining defers still run.
func TestSchedulerHookGoexit(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/rt"
type backend struct{}
//pu:scheduler
var scheduler backend
func(*backend)NewTask(rt.StackRequest)*rt.Task{println("backend exit");rt.ExitTask();return nil}
func(*backend)FreeTask(*rt.Task){}
func(*backend)Spawn(*rt.Task){}
func(*backend)Park(*rt.Task){}
func(*backend)Wake(*rt.Task){}
func(*backend)Yield(*rt.Task){}
func work(){}
func create(){defer println("direct cleanup");rt.Go(work,4096);panic("creation continued")}
func deferred(){defer println("remaining cleanup");defer rt.Go(work,4096)}
func main(){if !rt.CatchExitTask(create){panic("direct exit lost")};if !rt.CatchExitTask(deferred){panic("deferred exit lost")};println("caught both")}
`
	for _, level := range []int{0, 2} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		cfg := build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), OptLevel: level, Stdlib: plutonium.Stdlib()}
		if err := build.Build(&cfg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, cfg.Output)
		output, err := cmd.CombinedOutput()
		cancel()
		const want = "backend exit\ndirect cleanup\nbackend exit\nremaining cleanup\ncaught both\n"
		if err != nil || string(output) != want {
			t.Fatalf("O%d exit %v:\n%s", level, err, output)
		}
	}
}

func TestAllocatorCreationHookGoexit(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/alloc"
import "pluto/rt"
import "unsafe"
type counter struct{next alloc.Allocator}
var mine counter
func(c *counter)Alloc(n,a uintptr)unsafe.Pointer{println("allocator exit");rt.ExitTask();return nil}
func(c *counter)Free(p unsafe.Pointer,n,a uintptr){c.next.Free(p,n,a)}
func work(){}
func create(){defer println("creation cleanup");rt.Go(work,4096,&mine);panic("creation continued")}
func main(){mine.next=alloc.Default();if !rt.CatchExitTask(create){panic("allocator exit lost")};println("caught allocator")}
`
	runGoexitHook(t, source, "allocator exit\ncreation cleanup\ncaught allocator\n", plutonium.Stdlib())
}

func TestSourceLateHookGoexit(t *testing.T) {
	testguard.Parallel(t)
	lib := plutonium.Stdlib()
	const path = "pluto/rt/timer.p"
	data, err := fs.ReadFile(lib, path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "func Sleep(ns int64)") != 1 {
		t.Fatal("expected exactly one Sleep declaration")
	}
	source := strings.Replace(string(data), "func Sleep(ns int64)", `func Sleep(ns int64){println("source hook exit");ExitTask()}`, 1)
	if source == string(data) {
		t.Fatal("Sleep declaration missing")
	}
	const program = `package main
import "pluto/rt"
func call(){defer println("source cleanup");rt.Sleep(0);panic("source continued")}
func main(){if !rt.CatchExitTask(call){panic("source exit lost")};println("caught source")}
`
	runGoexitHook(t, program, "source hook exit\nsource cleanup\ncaught source\n", goexitHookOverlay{lib, fstest.MapFS{path: &fstest.MapFile{Data: []byte(source)}}})
}

func TestBoundLateHookGoexit(t *testing.T) {
	testguard.Parallel(t)
	lib := plutonium.Stdlib()
	overrides := fstest.MapFS{}
	for _, hook := range []struct{ path, name, receipt string }{
		{"pluto/rt/timer_linux.p", "sleepDirect", "bound hook exit"},
		{"pluto/rt/timer.p", "sleepParked", "parked hook exit"},
	} {
		data, err := fs.ReadFile(lib, hook.path)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(string(data), "func "+hook.name+"(ns int64) {")
		if start < 0 {
			t.Fatalf("%s missing", hook.name)
		}
		brace := start + strings.Index(string(data[start:]), "{")
		depth, end := 1, brace+1
		for ; end < len(data) && depth != 0; end++ {
			switch data[end] {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		if depth != 0 {
			t.Fatalf("%s body is not balanced", hook.name)
		}
		body := "{println(\"" + hook.receipt + "\");ExitTask()}"
		overrides[hook.path] = &fstest.MapFile{Data: []byte(string(data[:brace]) + body + string(data[end:]))}
	}
	const program = `package main
import "pluto/rt"
func direct(){defer println("direct cleanup");rt.Sleep(0);panic("direct continued")}
func saved(){defer println("saved cleanup");f:=rt.Sleep;f(0);panic("saved continued")}
func deferred(){defer println("deferred cleanup");f:=rt.Sleep;defer f(0)}
func work(){}
func main(){TASK;if !rt.CatchExitTask(direct){panic("direct exit lost")};if !rt.CatchExitTask(saved){panic("saved exit lost")};if !rt.CatchExitTask(deferred){panic("deferred exit lost")};println("caught hooks")}
`
	// A program that starts a task has a scheduler, so Sleep parks; one that
	// starts none sleeps directly, and ending main with ExitTask needs no
	// scheduler either (language spec section 19.5).
	const parked = "parked hook exit\ndirect cleanup\nparked hook exit\nsaved cleanup\nparked hook exit\ndeferred cleanup\ncaught hooks\n"
	const direct = "bound hook exit\ndirect cleanup\nbound hook exit\nsaved cleanup\nbound hook exit\ndeferred cleanup\ncaught hooks\n"
	runGoexitHook(t, strings.Replace(program, "TASK;", "task work();", 1), parked, goexitHookOverlay{lib, overrides})
	runGoexitHook(t, strings.Replace(program, "TASK;", "", 1), direct, goexitHookOverlay{lib, overrides})
}

type goexitHookOverlay struct {
	fs.FS
	overrides fstest.MapFS
}

func (o goexitHookOverlay) Open(name string) (fs.File, error) {
	if _, ok := o.overrides[name]; ok {
		return o.overrides.Open(name)
	}
	return o.FS.Open(name)
}

func runGoexitHook(t *testing.T, source, want string, lib fs.FS) {
	t.Helper()
	for _, level := range []int{0, 2} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		cfg := build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), OptLevel: level, Stdlib: lib}
		if err := build.Build(&cfg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		output, err := exec.CommandContext(ctx, cfg.Output).CombinedOutput()
		cancel()
		if err != nil || string(output) != want {
			t.Fatalf("O%d exit %v:\n%s", level, err, output)
		}
	}
}
