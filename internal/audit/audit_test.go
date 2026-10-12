package audit

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

var update = flag.Bool("update", false, "rewrite unsafe-audit expected reports")

func golden(t *testing.T, name, out string) {
	t.Helper()
	path := filepath.Join("testdata", name+".out")
	if *update {
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out != string(want) {
		t.Fatalf("report differs from %s:\n%s\nwant:\n%s", path, out, want)
	}
}

func sourceProgram(t *testing.T, src string) *load.Program {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs("../../lib")
	if err != nil {
		t.Fatal(err)
	}
	p, err := load.LoadPackage(&load.Config{Stdlib: os.DirFS(lib), Tags: load.BuildTags(false, false, nil), Sizes: types2.SizesFor("gc", "amd64")}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func report(t *testing.T, src string) string {
	t.Helper()
	p := sourceProgram(t, src)
	var out bytes.Buffer
	if err := WriteSelected(&out, p, []*load.Package{p.Main}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestSourceGraph(t *testing.T) {
	t.Parallel()
	src := `package sample
import u "unsafe"
//pu:nochecks
func raw() {}
func wrapper() { raw() }
func outer() { wrapper() }
func recursive() { recursive(); wrapper() }
func callback(f func()) { f() }
func pass() { callback(raw) }
func store() func() { return func() { _ = u.Sizeof(1) } }
func invoke() { store()() }
func unused() { _ = u.Add(nil, 0) }
type I interface { Run() }
type S struct{}
//pu:nochecks
func (S) Run() {}
func dynamic(i I) { i.Run() }
func methodValue(i I) { f := i.Run; f() }
func normal() { Sizeof := func(int) int { return 1 }; _ = Sizeof(2) }
func deferred() { defer raw() }
func spawned() { task raw() }
type O owner struct{X int}
//pu:nochecks
func (*O) Drop() {}
func cleanup() { o := O{X:1}; _ = o }
`
	out := report(t, src)
	golden(t, "source_graph", out)
	for _, name := range []string{"raw", "unused", "S.Run", "store.func@"} {
		if !strings.Contains(out, "unsafe main."+name) {
			t.Errorf("missing unsafe %s:\n%s", name, out)
		}
	}
	for _, name := range []string{"wrapper", "outer", "recursive", "callback", "pass", "invoke", "dynamic", "methodValue", "deferred", "spawned", "cleanup"} {
		if !strings.Contains(out, "safe main."+name+" ") {
			t.Errorf("missing safe caller %s:\n%s", name, out)
		}
	}
	for _, name := range []string{"store", "normal"} {
		if strings.Contains(out, "safe main."+name+" ") || strings.Contains(out, "unsafe main."+name+" ") {
			t.Errorf("uncalled literal / shadowed operation attributed to %s:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "may call main.raw") || !strings.Contains(out, "drop main.*O.Drop") {
		t.Errorf("missing conservative call or implicit drop:\n%s", out)
	}
	for range 4 {
		if again := report(t, src); again != out {
			t.Fatalf("nondeterministic report:\n%s\n%s", out, again)
		}
	}
}

func TestUnsafeCategories(t *testing.T) {
	t.Parallel()
	out := report(t, `package sample
import (u "unsafe"; "pluto/alloc"; "pluto/rt"; "pluto/arch/x86")
type Ptr = u.Pointer
type Pointers interface{~Ptr}
func genericPointer[T Pointers](p T) uintptr { return uintptr(p) }
func pointer(p Ptr) uintptr { return uintptr(p) }
func sizes() { var s struct{X int}; _ = u.Sizeof(s); _ = u.Alignof(s); _ = u.Offsetof(s.X) }
func primitives(p *byte,b []byte,s string) { _=u.Add(nil,0); _=u.Slice(p,0); _=u.String(p,0); _=u.SliceData(b); _=u.StringData(s); u.Drop(p) }
func unchecked() { p:=alloc.New[int](); alloc.Free(p); _=alloc.Raw(0,1) }
func direct(a alloc.Allocator) { p:=a.Alloc(0,1); a.Free(p,0,1) }
func saved(a alloc.Allocator) { f:=a.Alloc; _=f(0,1) }
func uninvoked(a alloc.Allocator) { _=a.Alloc }
func pinned(p *int) *int { return rt.Pinned(p) }
func arch() { x86.Pause() }
//pu:noescape
//pu:extern foreign
func foreign(p *int)
type Fake struct{}
func (Fake) Alloc() {}
func unrelated() { var f Fake; f.Alloc() }
`)
	golden(t, "categories", out)
	for _, reason := range []string{"unsafe.Pointer", "unsafe.Sizeof (layout query)", "unsafe.Alignof (layout query)", "unsafe.Offsetof (layout query)", "unsafe.Add", "unsafe.Slice", "unsafe.String", "unsafe.SliceData", "unsafe.StringData", "unsafe.Drop", "pluto/alloc.New", "pluto/alloc.Free", "pluto/alloc.Raw", "direct Allocator.Alloc", "direct Allocator.Free", "may invoke direct Allocator.Alloc", "pluto/rt.Pinned", "pluto/arch/x86.Pause", "function without a body", "//pu:noescape", "//pu:extern"} {
		if !strings.Contains(out, reason) {
			t.Errorf("missing %q:\n%s", reason, out)
		}
	}
	for _, name := range []string{"uninvoked", "unrelated"} {
		if strings.Contains(out, "main."+name+" ") {
			t.Errorf("unexpected report for %s:\n%s", name, out)
		}
	}
}

func TestCountedCleanup(t *testing.T) {
	t.Parallel()
	out := report(t, `package sample
import "pluto/alloc"
type E owner struct{X int}
//pu:nochecks
func (*E) Drop() {}
func (*E) Error() string { return "error" }
func fresh() E { return E{X:1} }
var kept = fresh()
var _ = fresh()
func named() (out E) { out=E{X:1}; return }
func forward() E { out:=E{X:1}; return out }
func forwardNested() E { { out:=E{X:1}; return out } }
func branch(flag bool) E { out:=E{X:1}; if flag {return out};return E{} }
func cleanup() { s:=alloc.Synced[E](); e:=error(s); _=e }
func container() { m:=make(map[int]synced[E]); m[0]=alloc.Synced[E](); _=m }
func channel() { c:=make(chan synced[E],1); c<-alloc.Synced[E](); _=c }
type Ow owner interface{}
func dynamic() { o:=Ow(E{X:1}); _=o }
`)
	golden(t, "counted_cleanup", out)
	for _, name := range []string{"cleanup", "container", "channel", "dynamic", "branch"} {
		if !strings.Contains(out, "safe main."+name+" ") {
			t.Errorf("missing cleanup %s:\n%s", name, out)
		}
	}
	if strings.Contains(out, "safe main.fresh ") || strings.Contains(out, "safe main.named ") || strings.Contains(out, "safe main.forward ") || strings.Contains(out, "safe main.forwardNested ") {
		t.Errorf("returned owner incorrectly dropped:\n%s", out)
	}
	if strings.Contains(out, "initializer@./main.p:8:") {
		t.Errorf("retained package owner incorrectly dropped:\n%s", out)
	}
	if !strings.Contains(out, "initializer@./main.p:9:") {
		t.Errorf("blank initializer cleanup missing:\n%s", out)
	}
}

func TestGenericAndPromotedCalls(t *testing.T) {
	t.Parallel()
	out := report(t, `package sample
//pu:nochecks
func raw[T any](v T) {}
func callback[T any](f func(T),v T) { f(v) }
func pass() { callback(raw[int],1) }
type Inner[T any] struct{}
//pu:nochecks
func (Inner[T]) Run(v T) {}
type Outer struct{Inner[int]}
type I interface{Run(int)}
func promoted(o Outer) { o.Run(1) }
func interfaceCall(i I) { i.Run(1) }
func expression(i I) { f:=I.Run; f(i,1) }
type Iter struct{}
func (Iter) All() func(func(int) bool) { return func(y func(int) bool) { raw(1); y(1) } }
func iteration() { for range (Iter{}) {} }
`)
	golden(t, "generic_promoted", out)
	for _, name := range []string{"callback", "pass", "promoted", "interfaceCall", "expression", "iteration"} {
		if !strings.Contains(out, "safe main."+name+" ") {
			t.Errorf("missing caller %s:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "may iterate main.Iter.All.func@") {
		t.Errorf("iterator closure path missing:\n%s", out)
	}
}

func TestCleanupOperations(t *testing.T) {
	t.Parallel()
	out := report(t, `package sample
import "pluto/alloc"
type E owner struct{X int}
//pu:nochecks
func (*E) Drop() {}
func (*E) Error() string { return "error" }
var global = make(map[int]synced[E])
func factory() synced[E] { return alloc.Synced[E]() }
var kept = factory()
func observe(s synced[E],e error,m map[int]synced[E],c chan synced[E]) {}
func discard() { _=factory() }
func generic[T any](fresh func() T) { _=fresh() }
func genericCall() { generic(factory) }
func clearOwners(s []E) { clear(s) }
func clearCounted(s []synced[E]) { clear(s) }
func copyCounted(dst,src []synced[E]) { copy(dst,src) }
func deleteMap() { delete(global,1) }
func deferred(s []synced[E]) { defer clear(s); defer copy(s,s); defer delete(global,1) }
`)
	golden(t, "cleanup_operations", out)
	for _, name := range []string{"discard", "generic", "genericCall", "clearOwners", "clearCounted", "copyCounted", "deleteMap", "deferred"} {
		if !strings.Contains(out, "safe main."+name+" ") {
			t.Errorf("missing cleanup %s:\n%s", name, out)
		}
	}
	if strings.Contains(out, "main.observe ") {
		t.Errorf("borrowed parameters falsely cleaned up:\n%s", out)
	}
	// Factory and retained initializer may call allocator implementation,
	// but they cannot run E.Drop themselves.
	for _, chunk := range strings.Split(out, "  safe ") {
		if strings.HasPrefix(chunk, "main.factory ") || strings.HasPrefix(chunk, "main.initializer@./main.p:9:") {
			if strings.Contains(chunk, "calls main.*E.Drop") {
				t.Errorf("retained counted result falsely cleaned up:\n%s", chunk)
			}
		}
	}
}

func TestRetainedAndWeakReferences(t *testing.T) {
	t.Parallel()
	out := report(t, `package sample
import "pluto/alloc"
type E owner struct{X int}
//pu:nochecks
func (*E) Drop() {}
func factory() synced[E] { return alloc.Synced[E]() }
func pair() (synced[E],synced[E]) { return factory(),factory() }
type H struct{p synced[E]}
var parens=(factory())
var aggregate=H{p:factory()}
var a,b=pair()
var _,c=pair()
func weakOnly(w weak[synced[E]]) { copy:=w; _=copy }
func recursiveA[T any](f func()T) { recursiveB(f) }
func recursiveB[U any](f func()U) { recursiveA(f); _=f() }
func reach() { recursiveA(factory) }
type Maker[T any] struct{f func()T}
func (m Maker[T]) Discard(){ _=m.f() }
type Promoted struct{Maker[synced[E]]}
func method() { m:=Maker[synced[E]]{factory}; m.Discard() }
func promoted() { m:=Promoted{Maker[synced[E]]{factory}}; m.Discard() }
`)
	golden(t, "retained_weak_recursive", out)
	if strings.Contains(out, "main.weakOnly ") {
		t.Errorf("weak-only release drops payload:\n%s", out)
	}
	for _, chunk := range strings.Split(out, "  safe ") {
		for _, prefix := range []string{"main.initializer@./main.p:9:", "main.initializer@./main.p:10:", "main.initializer@./main.p:11:"} {
			if strings.HasPrefix(chunk, prefix) && strings.Contains(chunk, "calls main.*E.Drop") {
				t.Errorf("retained global has payload drop:\n%s", chunk)
			}
		}
	}
	for _, name := range []string{"recursiveA", "recursiveB", "reach", "Maker[T].Discard", "method", "promoted"} {
		if !strings.Contains(out, "safe main."+name+" ") {
			t.Errorf("recursive generic cleanup missing %s:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "main.initializer@./main.p:12:") {
		t.Errorf("mixed blank cleanup missing:\n%s", out)
	}
}

type failedWriter struct{}

func TestLiteralSignature(t *testing.T) {
	t.Parallel()
	out := report(t, `package sample
import "unsafe"
type E owner struct{X int}
//pu:nochecks
func (*E) Drop() {}
func use() { f:=func(e E){}; f(E{1}) }
func pointer() { f:=func(p unsafe.Pointer){}; f(nil) }
`)
	if !strings.Contains(out, "safe main.use.func@") || !strings.Contains(out, "may drop main.*E.Drop") {
		t.Fatalf("literal owner parameter cleanup absent:\n%s", out)
	}
	if !strings.Contains(out, "unsafe.Pointer parameter") {
		t.Fatalf("literal pointer parameter absent:\n%s", out)
	}
}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("closed report") }

func TestAuditFailures(t *testing.T) {
	t.Parallel()
	p := sourceProgram(t, "package p\n//pu:nochecks\nfunc f() {}\n")
	if err := Write(failedWriter{}, p); err == nil || err.Error() != "closed report" {
		t.Fatalf("writer failure: %v", err)
	}
	for _, src := range []string{
		"package p;func f()*int{x:=1;return &x}",
		"package p;type O owner struct{X int};func consume(o O){};func f(){o:=O{1};consume(o);consume(o)}",
		"package p;var x int;func worker(){x++};func main(){task worker();task worker()}",
	} {
		p := sourceProgram(t, src)
		var b bytes.Buffer
		if err := Write(&b, p); err == nil {
			t.Fatalf("invalid source accepted: %s\n%s", src, b.String())
		}
	}
}
