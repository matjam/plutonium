package build_test

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// A broken task/callback drain must fail this bounded oracle rather than wait
// for the full suite's timeout (the canceled timer deliberately has a long deadline).
func runNormalShutdown(t *testing.T, source string, opt int) (string, int) {
	t.Helper()
	prog := buildConc(t, source, build.Config{OptLevel: opt, GenCheck: true})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, prog).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("shutdown did not complete: %v; output %q", ctx.Err(), out)
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return string(out), exited.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// The package schedule consumes current values in place. Hidden static objects
// own their fields once; their borrowed aliases do not add ownership.
func TestNormalShutdownValues(t *testing.T) {
	testguard.Parallel(t)
	for _, tc := range []struct{ name, source, want string }{
		{"staticReceiver", `package main
type item owner struct{id int}
var published *item
type observer owner struct{live bool}
func(o *observer)Drop(){if o.live{println("static",published==&last,published.id==0);o.live=false}}
var first=observer{live:true}
func(p *item)Drop(){if p.id!=0{println("drop",p.id);published=p}}
var last=item{id:2}
func main(){}
`, "drop 2\nstatic true true\n"},
		{"pendingSlot", `package main
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("item",p.id)}}
var pending item
type setter owner struct{live bool}
func(s *setter)Drop(){if s.live{pending=item{id:8};s.live=false}}
var late=setter{live:true}
func main(){}
`, "item 8\n"},
		{"zeroDependency", `package main
type backing owner struct{id int}
func(b *backing)Drop(){if b.id!=0{println("backing",b.id)}}
type client owner struct{live bool}
func(c *client)Drop(){if c.live{println("client",storage.id);c.live=false}}
var use=makeClient()
var storage backing
func makeClient()client{_ = storage.id;return client{live:true}}
func main(){storage=backing{id:9}}
`, "client 9\nbacking 9\n"},
		{"hiddenCounted", `package main
import "pluto/alloc"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("last reference",p.id)}}
type holder struct{p synced[item]}
var first, second synced[item]
var hidden=&holder{}
func main(){first=alloc.Synced[item]();first.id=6;second=first;hidden.p=first}
`, "last reference 6\n"},
		{"borrowedInterface", `package main
import "pluto/alloc"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("last reference",p.id)}}
type receiver struct{id int;p synced[item]}
func(r receiver)Read()int{return r.p.id}
func makeItem()synced[item]{p:=alloc.Synced[item]();p.id=6;return p}
var view interface{Read()int}=receiver{p:makeItem()}
func main(){println("read",view.Read())}
`, "read 6\nlast reference 6\n"},
		{"owningInterface", `package main
import "pluto/alloc"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("last reference",p.id)}}
type receiver owner struct{id int;p synced[item]}
type resource owner interface{}
func makeItem()synced[item]{p:=alloc.Synced[item]();p.id=6;return p}
var view resource=receiver{p:makeItem()}
func main(){}
`, "last reference 6\n"},
		{"methodReceiver", `package main
import "pluto/alloc"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("last reference",p.id)}}
type receiver struct{id int;p synced[item]}
func(r receiver)Read()int{return r.p.id}
func makeItem()synced[item]{p:=alloc.Synced[item]();p.id=6;return p}
var read=receiver{p:makeItem()}.Read
func main(){println("read",read())}
`, "read 6\nlast reference 6\n"},
		{"callerRegion", `package main
import "pluto/alloc"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("last reference",p.id)}}
func makeItem()synced[item]{p:=alloc.Synced[item]();p.id=6;return p}
func reader(p synced[item])func()int{return func()int{return p.id}}
var read=reader(makeItem())
func main(){println("read",read())}
`, "read 6\nlast reference 6\n"},
		{"pendingParentReplacement", `package main
import "pluto/alloc"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("drop",p.id)}}
func makeItem(id int)synced[item]{p:=alloc.Synced[item]();p.id=id;return p}
type parent owner struct{early int;live bool;later item;counted synced[item]}
func(p *parent)Drop(){if p.live{
 p.later=item{id:2}
 p.counted=makeItem(4)
 p.early=9
 println("parent",p.early)
 p.live=false
}}
var root=parent{live:true,later:item{id:1},counted:makeItem(3)}
func main(){}
`, "drop 1\ndrop 3\nparent 9\ndrop 2\ndrop 4\n"},
		{"cleanupGoexit", `package main
import "pluto/rt"
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("drop",p.id);if p.id==2{rt.ExitTask();println("unexpected")}}}
type outer owner struct{id int;field item}
func(p *outer)Drop(){if p.id!=0{println("outer",p.id);rt.ExitTask();println("unexpected")}}
var first=item{id:1}
var second=item{id:2}
var third=outer{id:3,field:item{id:4}}
func main()int64{return 17}
`, "outer 3\ndrop 4\ndrop 2\ndrop 1\n"},
	} {
		for _, opt := range []int{0, 2} {
			t.Run(tc.name+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runNormalShutdown(t, tc.source, opt)
				wantStatus := 0
				if tc.name == "cleanupGoexit" {
					wantStatus = 17
				}
				if status != wantStatus || out != tc.want {
					t.Fatalf("status %d, output %q; want status %d, %q", status, out, wantStatus, tc.want)
				}
			})
		}
	}
}

// A later sibling's destructor cannot revive already-completed inline storage,
// even through an alias or by writing the zero value.
func TestNormalShutdownFinalizedWrites(t *testing.T) {
	testguard.Parallel(t)
	for _, tc := range []struct{ name, action string }{
		{"scalarZeroAlias", `*scalar=0`},
		{"ownerZeroAlias", `*target=item{}`},
		{"takeAlias", `v:=take(target);_=v`},
		{"clearAlias", `clear(root.first[:])`},
		{"deferredClearAlias", `deferredClear()`},
	} {
		source := `package main
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("first",p.id)}}
type writer owner struct{live bool}
func(w *writer)Drop(){if w.live{` + tc.action + `;println("unexpected committed")}}
type pair owner struct{first [1]item;last writer}
var target *item
var scalar *int
var aliases []item
var root=pair{first:[1]item{{id:1}},last:writer{live:true}}
func deferredClear(){defer clear(aliases)}
func main(){target=&root.first[0];scalar=&root.first[0].id;aliases=root.first[:]}
`
		for _, opt := range []int{0, 2} {
			t.Run(tc.name+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runNormalShutdown(t, source, opt)
				const want = "first 1\npanic: write to a finalized package resource during shutdown\n"
				if status != 2 || !strings.HasPrefix(out, want) || strings.Contains(out, "unexpected committed") {
					t.Fatalf("status %d, output %q; want prefix %q and no committed write", status, out, want)
				}
			})
		}
	}
}

func TestNormalShutdownFinalizedCountedWrite(t *testing.T) {
	testguard.Parallel(t)
	for _, tc := range []struct{ name, action, want string }{
		{"assignment", `*target=text()`, "rhs\n"},
		{"deferredCopyAlias", `deferredCopy()`, ""},
	} {
		source := `package main
import "pluto/strconv"
var target *string
var aliases []string
type writer owner struct{live bool}
func(w *writer)Drop(){if w.live{` + tc.action + `;println("unexpected committed")}}
type pair owner struct{first [1]string;last writer}
var root pair
func text()string{println("rhs");return strconv.Itoa(91)}
func deferredCopy(){defer copy(aliases,[]string{"replacement"})}
func main(){root.first[0]=strconv.Itoa(17);root.last=writer{live:true};target=&root.first[0];aliases=root.first[:]}
`
		for _, opt := range []int{0, 2} {
			t.Run(tc.name+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runNormalShutdown(t, source, opt)
				want := tc.want + "panic: write to a finalized package resource during shutdown\n"
				if status != 2 || !strings.HasPrefix(out, want) || strings.Contains(out, "unexpected committed") {
					t.Fatalf("status %d, output %q; want prefix %q and no committed write", status, out, want)
				}
			})
		}
	}
}

// Assembly-backed atomics must check the actual destination, including when
// reached through a function value. Adjacent unowned static POD remains writable.
func TestNormalShutdownAtomicWrites(t *testing.T) {
	testguard.Parallel(t)
	for _, live := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			destination := "victim"
			if live {
				destination = "neighbor"
			}
			action := `atomic.StoreUint32(small,7);println("write",atomic.LoadUint32(small)==7)`
			if wide {
				action = `store64(big,9);println("write",atomic.LoadUint64(big)==9)`
			}
			source := `package main
import "pluto/sync/atomic"
type payload owner struct{live bool;small uint32;big uint64}
func(p *payload)Drop(){if p.live{println("payload");p.live=false}}
var neighbor struct{small uint32;big uint64}
var small *uint32
var big *uint64
var store64=atomic.StoreUint64
type observer owner struct{live bool}
func(o *observer)Drop(){if o.live{` + action + `;o.live=false}}
var final=observer{live:true}
var victim=payload{live:true}
func main(){small=&` + destination + `.small;big=&` + destination + `.big}
`
			for _, opt := range []int{0, 2} {
				t.Run("live"+strconv.FormatBool(live)+"/wide"+strconv.FormatBool(wide)+"/O"+strconv.Itoa(opt), func(t *testing.T) {
					testguard.Parallel(t)
					out, status := runNormalShutdown(t, source, opt)
					if live {
						if status != 0 || out != "payload\nwrite true\n" {
							t.Fatalf("live POD: status %d, output %q", status, out)
						}
					} else {
						const want = "payload\npanic: write to a finalized package resource during shutdown\n"
						if status != 2 || !strings.HasPrefix(out, want) || strings.Contains(out, "write true") {
							t.Fatalf("completed owner: status %d, output %q; want %q", status, out, want)
						}
					}
				})
			}
		}
	}
}

// os.File.Read passes a typed byte slice through the syscall ABI. Finalized
// owner backing must be rejected before the kernel can overwrite it.
func TestNormalShutdownFileRead(t *testing.T) {
	testguard.Parallel(t)
	for _, live := range []bool{false, true} {
		destination := "victim.buf[:]"
		if live {
			destination = "neighbor[:]"
		}
		source := `package main
import "pluto/os"
var neighbor [4]byte
var target []byte
type payload owner struct{live bool;buf [4]byte}
func(p *payload)Drop(){if p.live{println("payload");p.live=false}}
type observer owner struct{live bool}
func(o *observer)Drop(){if o.live{
 f,e:=os.Open("/dev/zero");if e!=nil{panic("open zero failed")};defer f.Close()
 n,e:=f.Read(target);println("read",n==4,e==nil,target[0]==0);o.live=false
}}
var final=observer{live:true}
var victim=payload{live:true}
func main(){neighbor[0]=9;victim.buf[0]=9;target=` + destination + `}
`
		for _, opt := range []int{0, 2} {
			t.Run("live"+strconv.FormatBool(live)+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runNormalShutdown(t, source, opt)
				if live {
					if status != 0 || out != "payload\nread true true true\n" {
						t.Fatalf("live POD: status %d, output %q", status, out)
					}
				} else {
					const want = "payload\npanic: write to a finalized package resource during shutdown\n"
					if status != 2 || !strings.HasPrefix(out, want) || strings.Contains(out, "read true") {
						t.Fatalf("completed owner: status %d, output %q; want %q", status, out, want)
					}
				}
			})
		}
	}
}

// Sections 15.4 and 18.6 require ordinary local cleanup before package cleanup.
func TestNormalShutdownBasic(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
type resource owner struct { id int }
func (r *resource) Drop() { if r.id != 0 { println("drop", r.id); r.id = 0 } }
var first = resource{id: 1}
var second resource
func main() int64 {
 second = resource{id: 2}
 local := resource{id: 3}
 defer println("defer")
 println("main",local.id)
 return 7
}
`
	for _, opt := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runNormalShutdown(t, source, opt)
			const want = "main 3\ndefer\ndrop 3\ndrop 2\ndrop 1\n"
			if status != 7 || out != want {
				t.Fatalf("status %d, output %q; want status 7, %q", status, out, want)
			}
		})
	}
}

// Shutdown waits for descendants and the explicit stack allocator's release,
// rather than treating a task's returned frame as complete retirement.
func TestNormalShutdownTaskRetirement(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import("pluto/alloc";"pluto/rt";"pluto/sync/atomic";"unsafe")
type stackAllocator struct { base alloc.Allocator }
var stack stackAllocator
var done, stackFrees atomic.Int32
func(a *stackAllocator)Alloc(n,k uintptr)unsafe.Pointer{return a.base.Alloc(n,k)}
func(a *stackAllocator)Free(p unsafe.Pointer,n,k uintptr){a.base.Free(p,n,k);stackFrees.Add(1)}
type check owner struct { live bool }
func(c *check)Drop(){if c.live{println("retired",done.Load()==2,stackFrees.Load()==1);c.live=false}}
var final = check{live:true}
func child(){done.Add(1)}
func worker(){task child();done.Add(1)}
func main(){stack.base=alloc.Default();rt.Go(worker,64<<10,&stack)}
`
	for _, opt := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runNormalShutdown(t, source, opt)
			if status != 0 || out != "retired true true\n" {
				t.Fatalf("status %d, output %q; want retired true true", status, out)
			}
		})
	}
}

// The pending callback is package-held, so main's return must neither cancel it
// nor abandon it. Explicit cancellation, however, must not wait for its deadline.
func TestNormalShutdownAfterFunc(t *testing.T) {
	testguard.Parallel(t)
	for _, mode := range []string{"pending", "canceled", "exitCallback"} {
		body := `timer=time.AfterFunc(time.Millisecond,fire,&heap)`
		fire, extraImport := `fired.Add(1)`, ""
		name := "cancelfalse"
		want := "callback true\ntimer disposed true\n"
		switch mode {
		case "canceled":
			name = "canceltrue"
			body = `timer=time.AfterFunc(time.Hour,fire,&heap);println("stopped",timer.Stop())`
			want = "stopped true\ncallback false\ntimer disposed true\n"
		case "exitCallback":
			name = mode
			extraImport = `;"pluto/rt"`
			fire = `defer println("callback defer");fired.Add(1);rt.ExitTask();println("unexpected callback tail")`
			want = "callback defer\ncallback true\ntimer disposed true\n"
		}
		source := `package main
import("pluto/alloc";"pluto/time";"pluto/sync/atomic";"unsafe"` + extraImport + `)
type counter struct{base alloc.Allocator}
var heap counter
var fired, live atomic.Int32
func(c *counter)Alloc(n,k uintptr)unsafe.Pointer{p:=c.base.Alloc(n,k);if p!=nil{live.Add(1)};return p}
func(c *counter)Free(p unsafe.Pointer,n,k uintptr){c.base.Free(p,n,k);if p!=nil{live.Add(-1)}}
type check owner struct { live bool }
func(c *check)Drop(){if c.live{println("callback",fired.Load()==1);println("timer disposed",live.Load()==0);c.live=false}}
var final=check{live:true}
var timer synced[time.Timer]
func fire(){` + fire + `}
func main(){heap.base=alloc.Default();` + body + `}
`
		for _, opt := range []int{0, 2} {
			t.Run(name+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runNormalShutdown(t, source, opt)
				if status != 0 || out != want {
					t.Fatalf("status %d, output %q; want %q", status, out, want)
				}
			})
		}
	}
}
