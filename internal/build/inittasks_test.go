package build_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// Initialization must finish before another task can observe package state
// (section 15.2). Calls and cleanup are checked before optimization.
func TestInitializationTasks(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct{ name, source string }{
		{"direct", `func init() { task worker() }`},
		{"variable", `var value = start(); func start() int { task worker(); return 1 }`},
		{"transitive", `func init() { helper() }; func helper() { start() }; func start() { task worker() }`},
		{"recursive", `func init() { helper(false) }; func helper(b bool) { if b { helper(false) }; task worker() }`},
		{"function value", `var callback = start; func init() { callback() }; func start() { task worker() }`},
		{"literal call", `var value = func() int { task worker(); return 1 }()`},
		{"scope literal", `func init() { var s sync.Scope; s.Task(func() { worker() }) }`},
		{"returned closure", `func init() { factory()() }; func factory() func() { return func() { task worker() } }`},
		{"deferred", `func init() { defer start() }; func start() { task worker() }`},
		{"wait then start", `func init() { time.Sleep(1); task worker() }`},
		{"scope", `func init() { var s sync.Scope; s.Task(worker) }`},
		{"scope pointer", `func init() { var s sync.Scope; (&s).Task(worker) }`},
		{"wait group", `func init() { var w sync.WaitGroup; w.Task(worker) }`},
		{"runtime", `func init() { rt.Go(worker, 0) }`},
		{"timer", `func init() { _ = time.AfterFunc(1, worker) }`},
		{"interface", `type Starter interface { Start() }; type S struct{}; func (S) Start() { task worker() }; var s Starter = S{}; func init() { s.Start() }`},
		{"method value", `type S struct{}; func (S) Start() { task worker() }; func init() { f := S{}.Start; f() }`},
		{"owner drop", `type S owner struct { active bool }; func (s *S) Drop() { if s.active { start() } }; func start() { task worker() }; func init() { s := S{true}; _ = s }`},
		{"owning interface drop", `type S owner struct { active bool }; func (s *S) Drop() { if s.active { start() } }; func start() { task worker() }; type I owner interface{}; func init() { var i I = S{true}; _ = i }`},
		{"counted error drop", `type S owner struct { active bool }; func (s *S) Drop() { if s.active { start() } }; func (s *S) Error() string { return "bad" }; func start() { task worker() }; func init() { p := alloc.Synced[S](); p.active = true; var e error = p; _ = e }`},
		{"range function", `func seq(yield func(int) bool) { yield(1) }; func start() { task worker() }; func init() { for range seq { start() } }`},
		{"ring yield", `func start() { task worker() }; func init() { var r ring[int, 8]; r.Push(1); for range r.All() { start() } }`},
		{"extern export", "//pu:extern bridge\nfunc bridge()\n//pu:export bridge\nfunc exported() { task worker() }\nfunc init() { bridge() }"},
		{"blank variable", `var _ = start(); func start() int { task worker(); return 1 }`},
		{"multiple variables", `var a, b = start(); func start() (int, int) { task worker(); return 1, 2 }`},
		{"variable temporary drop", `type S owner struct { active bool; field int }; func (s *S) Drop() { if s.active { start() } }; func start() { task worker() }; func makeS() owned[S] { return new(S{true, 3}) }; var result = makeS().field`},
		{"blank owner drop", `type S owner struct { active bool }; func (s *S) Drop() { if s.active { start() } }; func start() { task worker() }; func makeS() owned[S] { return new(S{true}) }; var _ = makeS()`},
		{"blank owner receive", `type S owner struct { active bool }; func (s *S) Drop() { if s.active { start() } }; func start() { task worker() }; func makeChan() chan owned[S] { c := make(chan owned[S], 1); c <- new(S{true}); return c }; var ch = makeChan(); var _ = <-ch`},
		{"blank owner receive pair", `type S owner struct { active bool }; func (s *S) Drop() { if s.active { start() } }; func start() { task worker() }; func makeChan() chan owned[S] { c := make(chan owned[S], 1); c <- new(S{true}); return c }; var ch = makeChan(); var _, ok = <-ch`},
		{"blank owner assertion", `var marker int; type S owner struct { p *int }; func (s *S) Drop() { if s.p != nil { start() } }; func start() { task worker() }; type I owner interface{}; func makeI() I { return S{&marker} }; var _ = makeI().(S)`},
		{"blank owner assertion pair", `var marker int; type S owner struct { p *int }; func (s *S) Drop() { if s.p != nil { start() } }; func start() { task worker() }; type I owner interface{}; func makeI() I { return S{&marker} }; var _, ok = makeI().(S)`},
	}
	for _, tc := range cases {
		for _, level := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", tc.name, level), func(t *testing.T) {
				testguard.Parallel(t)
				src := "package main\nimport (\"pluto/rt\"; \"pluto/sync\"; \"pluto/time\"; \"pluto/alloc\")\nvar _ = rt.Go\nvar _ sync.WaitGroup\nvar _ = time.Sleep\nvar _ = alloc.Default\nfunc worker() {}\n" + tc.source + "\nfunc main() {}\n"
				_, err := buildIn(t.TempDir(), src, build.Config{OptLevel: level})
				if err == nil {
					t.Fatal("compiled initialization that starts a task")
				}
				if !strings.Contains(fullError(err), "E2587") {
					t.Fatalf("wrong diagnostic:\n%s", fullError(err))
				}
			})
		}
	}
}

func TestInitializationTasksBuildModes(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/rt"
func worker() {}
//pu:panic
func onPanic(p *rt.Panic) { for {} }
func main() {}
type scheduler struct{}
//pu:scheduler
var sched scheduler
func (scheduler) NewTask(req rt.StackRequest) *rt.Task { return nil }
func (scheduler) FreeTask(t *rt.Task) {}
func (scheduler) Spawn(t *rt.Task) {}
func (scheduler) Park(t *rt.Task) {}
func (scheduler) Wake(t *rt.Task) {}
func (scheduler) Yield(t *rt.Task) {}
func (scheduler) InInterrupt() bool { return false }
`
	for _, mode := range []string{"hosted", "freestanding", "kernel"} {
		for _, declaration := range []string{"func init() { task worker() }", "type S owner struct { active bool }; func (s *S) Drop() { if s.active { task worker() } }; var _ = new(S{true})"} {
			t.Run(mode+"/"+declaration, func(t *testing.T) {
				testguard.Parallel(t)
				_, err := buildIn(t.TempDir(), source+declaration, build.Config{CompileOnly: true, Freestanding: mode == "freestanding", Kernel: mode == "kernel"})
				if err == nil || !strings.Contains(fullError(err), "E2587") {
					t.Fatalf("missing task diagnostic: %v", err)
				}
			})
		}
	}
}

func TestInitializationTasksImportedPackage(t *testing.T) {
	testguard.Parallel(t)
	for _, declaration := range []string{"var Value = start()", "func init() { start() }"} {
		t.Run(declaration, func(t *testing.T) {
			testguard.Parallel(t)
			dir := t.TempDir()
			files := map[string]string{"pluto.mod": "module example.com/inittask\n", "other/main.p": "package other\n" + declaration + "\nfunc start() int { task worker(); return 1 }; func worker() {}\n"}
			for name, source := range files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := buildIn(dir, "package main\nimport _ \"example.com/inittask/other\"\nfunc main() {}\n", build.Config{OptLevel: 2})
			if err == nil || !strings.Contains(fullError(err), "E2587") {
				t.Fatalf("missing imported initializer diagnostic: %v", err)
			}
		})
	}
}

func TestInitializationStoresCallback(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main
import "pluto/time"
var callback = func() { task worker() }
var ready = pause()
func pause() bool { time.Sleep(1); return true }
func worker() {}
func main() { if !ready { panic("not initialized") }; callback(); println("ready") }
`
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := compileAndRun(t, src, level)
			if status != 0 || out != "ready\n" {
				t.Fatalf("status %d, output %q", status, out)
			}
		})
	}
}

func TestInitializationUnrelatedDrop(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main
import ("pluto/alloc"; "pluto/fmt")
type T owner struct{ active bool }
func (t *T) Drop() { if t.active { task worker() } }
func worker() {}
func helper() int { var e error = fmt.Errorf("bad %d", 1); _ = e; return 1 }
var ready = helper()
func main() { var unrelated any = alloc.Synced[T](); _ = unrelated; println(ready) }
`
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := compileAndRun(t, src, level)
			if status != 0 || out != "1\n" {
				t.Fatalf("status %d, output %q", status, out)
			}
		})
	}
}

func TestInitializationBlankOwners(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main
var drops int
var marker int
type S owner struct{ active *int }
func (s *S) Drop() { if s.active != nil { drops++ } }
func makeS() owned[S] { return new(S{&marker}) }
func pair() (owned[S], owned[S]) { return makeS(), makeS() }
var _ = makeS()
var afterFirst = drops
var _, kept = pair()
var afterPair = drops
func makeChan() chan owned[S] { c := make(chan owned[S], 2); c <- makeS(); c <- makeS(); return c }
var ch = makeChan()
var _ = <-ch
var _, received = <-ch
var afterReceive = drops
type I owner interface{}
func makeI() I { return S{&marker} }
var _ = makeI().(S)
var _, asserted = makeI().(S)
var afterAssert = drops
func init() { if drops != 6 { panic("blank owners not dropped exactly once") } }
func main() { _ = kept; println(afterFirst, afterPair, afterReceive, afterAssert, drops, kept.active != nil, received, asserted) }
`
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := compileAndRun(t, src, level)
			if status != 0 || out != "1 2 4 6 6 true true true\n" {
				t.Fatalf("status %d, output %q", status, out)
			}
		})
	}
}

func TestInitializationBlankCounted(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main
import ("pluto/alloc"; "pluto/fmt"; "unsafe")
type counter struct { next alloc.Allocator; allocs, frees int }
func (c *counter) Alloc(n, a uintptr) unsafe.Pointer { c.allocs++; return c.next.Alloc(n, a) }
func (c *counter) Free(p unsafe.Pointer, n, a uintptr) { c.frees++; c.next.Free(p, n, a) }
var c counter
var installed = install()
func install() bool { c.next = alloc.Default(); alloc.SetDefault(&c); return true }
func text() string { _ = installed; return fmt.Sprintf("value %d", 1) }
func failure() error { _ = installed; return fmt.Errorf("value %d", 1) }
func table() map[int]int { _ = installed; return make(map[int]int) }
func channel() chan int { _ = installed; return make(chan int, 1) }
func box() synced[int] { _ = installed; return alloc.Synced[int]() }
var _ = text()
var _ = failure()
var _ = table()
var _ = channel()
var _ = box()
var balanced = c.allocs > 0 && c.allocs == c.frees
func init() { if !balanced { panic("discarded counted initializer leaked") } }
func main() { println(balanced) }
`
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := compileAndRun(t, src, level)
			if status != 0 || out != "true\n" {
				t.Fatalf("status %d, output %q", status, out)
			}
		})
	}
}
