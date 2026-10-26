package build_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// Each case resumes on the same task after CatchGoexit. Required destruction
// completes in order and restores the existing runtime drop depth.
func TestGoexitCleanupBalance(t *testing.T) {
	testguard.Parallel(t)
	lib := plutonium.Stdlib()
	const overlayPath = "pluto/rt/goexit.p"
	data, err := fs.ReadFile(lib, overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(`
func TestCoreDropDepth()int32{return current().drops}
`)...)

	const source = `package main
import "pluto/alloc"
import "pluto/rt"
import "pluto/strings"
import "unsafe"
var _ alloc.Allocator
var events,deferred,evaluated,later int
var order [16]int
var orderLen int
var tail bool
var exitFree,exitNoop bool
type CallbackAllocator struct{next alloc.Allocator}
var callbackAlloc CallbackAllocator
func(a *CallbackAllocator)Alloc(n,b uintptr)unsafe.Pointer{return a.next.Alloc(n,b)}
func(a *CallbackAllocator)Free(p unsafe.Pointer,n,b uintptr){a.next.Free(p,n,b);if exitFree{record(10);rt.ExitTask()}}
func idle(){if exitNoop{rt.ExitTask()}}
type After owner struct{live bool}
func(a *After)Drop(){if a.live{a.live=false;idle();record(100)}}
type WeakCleanup owner struct{w weak[shared[int]];a After}
func(*WeakCleanup)Drop(){}
type ChannelCleanup owner struct{c chan int;a After}
func(*ChannelCleanup)Drop(){}
type StringCleanup owner struct{s string;a After}
func(*StringCleanup)Drop(){}
func freshWeak()weak[shared[int]]{callbackAlloc.next=alloc.Default();p:=alloc.Shared[int](&callbackAlloc);return weak[shared[int]](p)}
func freshChannel()chan int{callbackAlloc.next=alloc.Default();alloc.SetDefault(&callbackAlloc);c:=make(chan int,1);alloc.SetDefault(callbackAlloc.next);return c}
func freshTimedChannel()chan int{c:=freshChannel();_ = rt.ChanTimer(*(*unsafe.Pointer)(unsafe.Pointer(&c)),nil);return c}
func freshText()string{callbackAlloc.next=alloc.Default();alloc.SetDefault(&callbackAlloc);s:=strings.Repeat("x",17);alloc.SetDefault(callbackAlloc.next);return s}
type Holder owner struct{active bool;id int}
func record(id int){order[orderLen]=id;orderLen++;events+=id}
func(h *Holder)Drop(){if h.id!=0{record(h.id);h.id=0;if h.active{h.active=false;rt.ExitTask()}}}
func(*Holder)Error()string{return "holder"}
type Outer owner struct{x,y Holder}
func(o *Outer)Drop(){if o.x.id!=0||o.y.id!=0{record(10)}}
type Resource owner interface{}
type InterfaceOuter owner struct{x Resource;y Holder}
func(o *InterfaceOuter)Drop(){if o.x!=nil||o.y.id!=0{record(10)}}
func rhs()Holder{rt.ExitTask();return Holder{}}
func stamp()Holder{evaluated++;return Holder{}}
func exitInt()int{rt.ExitTask();return 0}
func fresh()Holder{return Holder{id:100}}
func freshPair()(Holder,Holder){return fresh(),Holder{id:200}}
type Bucket owner struct{x Holder}
func(*Bucket)Drop(){}
func(b *Bucket)Accept(v Holder){b.x=v;rt.ExitTask()}
func sink(h Holder,n int){_ = h.id;_ = n}
func produce()(Holder,int){return fresh(),exitInt()}
func makeError()error{p:=alloc.Synced[Holder]();p.active=true;p.id=1;return p}
func freshShared()shared[Holder]{p:=alloc.Shared[Holder]();p.id=100;return p}
func freshSynced()synced[Holder]{p:=alloc.Synced[Holder]();p.id=100;return p}
func freshError()error{p:=alloc.Synced[Holder]();p.id=100;return p}
func sinkShared(p shared[Holder],n int){_ = p;_ = n}
func sinkSynced(p synced[Holder],n int){_ = p;_ = n}
func sinkError(e error,n int){_ = e;_ = n}
func readBorrowed(v interface{}){_ = v}
func mark(){deferred++}
func check(name string,fn func(),want int,exits bool){events=0;deferred=0;evaluated=0;later=0;orderLen=0;tail=false;exitFree=false;dropBefore:=rt.TestCoreDropDepth();if rt.CatchExitTask(fn)!=exits{panic("wrong exit")};dropAfter:=rt.TestCoreDropDepth();println(name,dropAfter==dropBefore,events==want,deferred==1&&tail!=exits&&evaluated==0&&later==0);for i:=0;i<orderLen;i++{println(order[i])}}
`
	cases := []struct {
		name, body string
		events     int
		order      string
		normal     bool
	}{
		{"rhs", `var x,y Holder;_ = x.active;_ = y.active;x,y=rhs(),stamp()`, 0, "", false},
		{"oldOwner", `x:=Holder{active:true,id:1};_ = x.active;x=Holder{id:100}`, 101, "1\n100\n", false},
		{"oldShared", `p:=alloc.Shared[Holder]();p.active=true;p.id=1;p,later=nil,7`, 1, "1\n", false},
		{"explicitDrop", `x:=Holder{active:true,id:1};x.Drop()`, 1, "1\n", false},
		{"ownerField", `o:=Outer{x:Holder{active:true,id:1},y:Holder{id:100}};o.x=Holder{}`, 111, "1\n10\n100\n", false},
		{"nestedDrop", `o:=Outer{x:Holder{active:true,id:1},y:Holder{id:100}};o.Drop()`, 111, "10\n1\n100\n", false},
		{"countedError", `e:=makeError();e,later=nil,7;_ = e`, 1, "1\n", false},
		{"owningInterface", `o:=InterfaceOuter{x:Holder{active:true,id:1},y:Holder{id:100}};o.Drop()`, 111, "10\n1\n100\n", false},
		{"laterRHS", `var x Holder;var n int;_ = x.id;_ = n;x,n=fresh(),exitInt()`, 100, "100\n", false},
		{"callArgument", `sink(fresh(),exitInt())`, 100, "100\n", false},
		{"returnOperand", `x,n:=produce();_ = x.id;_ = n`, 100, "100\n", false},
		{"calleeTransfer", `b:=Bucket{};b.Accept(fresh())`, 100, "100\n", false},
		{"normalTuple", `a,b:=freshPair();o:=Outer{x:a,y:b};o.Drop()`, 310, "10\n100\n200\n", true},
		{"normalAggregate", `o:=Outer{x:fresh(),y:Holder{id:200}};o.Drop()`, 310, "10\n100\n200\n", true},
		{"normalInterface", `var r Resource=fresh();_ = r`, 100, "100\n", true},
		{"normalDefer", `defer sink(fresh(),0)`, 100, "100\n", true},
		{"normalBlank", `_,_ = freshPair()`, 300, "200\n100\n", true},
		{"sharedProducer", `sinkShared(freshShared(),exitInt())`, 100, "100\n", false},
		{"syncedProducer", `sinkSynced(freshSynced(),exitInt())`, 100, "100\n", false},
		{"errorProducer", `sinkError(freshError(),exitInt())`, 100, "100\n", false},
		{"borrowedUse", `readBorrowed(fresh())`, 100, "100\n", true},
		{"weakFreeCallback", `o:=WeakCleanup{w:freshWeak(),a:After{true}};exitFree=true;o.Drop()`, 110, "10\n100\n", false},
		{"channelFreeCallback", `o:=ChannelCleanup{c:freshChannel(),a:After{true}};exitFree=true;o.Drop()`, 110, "10\n100\n", false},
		{"stringFreeCallback", `o:=StringCleanup{s:freshText(),a:After{true}};exitFree=true;o.Drop()`, 110, "10\n100\n", false},
		{"stringStoreCallback", `s:=freshText();exitFree=true;s,later="",7;_ = s`, 10, "10\n", false},
		{"allocatedStringStoreCallback", `s:=freshText();replacement:=freshText();exitFree=true;s,later=replacement,7;_ = s`, 20, "10\n10\n", false},
		{"allocatedSharedStore", `p:=alloc.Shared[Holder]();p.active=true;p.id=1;replacement:=freshShared();p,later=replacement,7;_ = p`, 101, "1\n100\n", false},
		{"allocatedErrorStore", `e:=makeError();replacement:=freshError();e,later=replacement,7;_ = e`, 101, "1\n100\n", false},
		{"timerChannelFreeCallback", `o:=ChannelCleanup{c:freshTimedChannel(),a:After{true}};exitFree=true;o.Drop()`, 120, "10\n10\n100\n", false},
	}
	var program, main, expected strings.Builder
	program.WriteString(source)
	main.WriteString("func main(){\n")
	for i, test := range cases {
		fmt.Fprintf(&program, "func exercise%d(){defer mark();%s;tail=true}\n", i, test.body)
		fmt.Fprintf(&main, "check(%q,exercise%d,%d,%t)\n", test.name, i, test.events, !test.normal)
		fmt.Fprintf(&expected, "%s true true true\n%s", test.name, test.order)
	}
	main.WriteString("}\n")
	program.WriteString(main.String())
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(program.String()), 0644); err != nil {
				t.Fatal(err)
			}
			cfg := build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), OptLevel: level, Stdlib: goexitHookOverlay{lib, fstest.MapFS{overlayPath: &fstest.MapFile{Data: data}}}}
			if err := buildCfg(&cfg); err != nil {
				t.Fatal(err)
			}
			out, status := runDebugAlloc(t, cfg.Output, "")
			if status != 0 || out != expected.String() {
				t.Fatalf("exit %d, cleanup after CatchGoexit: %q", status, out)
			}
		})
	}
}
