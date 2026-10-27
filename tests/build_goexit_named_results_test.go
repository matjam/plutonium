// Ported from internal/build/goexit_named_results_test.go at the go-cutover tag.

package tests

import (
	"fmt"
	"testing"
)

// Named results remain held by their frame when Goexit prevents the caller
// from receiving them, including values assigned by a deferred call.
func TestGoexitNamedResults(t *testing.T) {
	t.Parallel()
	const source = `package main
import "pluto/alloc"
import "pluto/rt"
import "pluto/strings"
import "unsafe"
var drops,frees int
var exitDrop,exitFree bool
type Holder owner struct{id int}
func(h *Holder)Drop(){if h.id!=0{id:=h.id;drops+=id;h.id=0;if exitDrop&&id==1{rt.ExitTask()}}}
func(*Holder)Error()string{return "holder"}
type counting struct{next alloc.Allocator}
var heap counting
func(c *counting)Alloc(n,a uintptr)unsafe.Pointer{return c.next.Alloc(n,a)}
func(c *counting)Free(p unsafe.Pointer,n,a uintptr){c.next.Free(p,n,a);frees++;if exitFree{rt.ExitTask()}}
func fresh()shared[Holder]{p:=alloc.Shared[Holder]();p.id=100;return p}
func freshId(id int)shared[Holder]{p:=alloc.Shared[Holder]();p.id=id;return p}
func freshSync(id int)synced[Holder]{p:=alloc.Synced[Holder]();p.id=id;return p}
func namedOwner()(r Holder){r=Holder{id:100};rt.ExitTask();return}
func deferredOwner()(r Holder){defer func(){r=Holder{id:100}}();rt.ExitTask();return}
func namedShared()(r shared[Holder]){r=fresh();rt.ExitTask();return}
func deferredShared()(r shared[Holder]){defer func(){r=fresh()}();rt.ExitTask();return}
func namedError()(r error){r=freshSync(100);rt.ExitTask();return}
func deferredError()(r error){defer func(){r=freshSync(100)}();rt.ExitTask();return}
func namedString()(r string){r=strings.Repeat("x",17);rt.ExitTask();return}
func deferredString()(r string){defer func(){r=strings.Repeat("x",17)}();rt.ExitTask();return}
func normalOwner()(r Holder){r=Holder{id:100};return}
func normalShared()(r shared[Holder]){r=fresh();return}
func overwriteOwner()(r Holder){r=Holder{id:1};defer func(){r=Holder{id:100}}();rt.ExitTask();return}
func normalOverwriteOwner()(r Holder){r=Holder{id:1};defer func(){r=Holder{id:100}}();return}
func overwriteShared()(r shared[Holder]){r=fresh();defer func(){r=fresh()}();rt.ExitTask();return}
func mixed()(r Holder,p shared[Holder],e error){r,p,e=Holder{id:100},fresh(),freshSync(100);rt.ExitTask();return}
func region(exits bool)(r shared[Holder],f func()int){r=fresh();f=func()int{return r.id};if exits{rt.ExitTask()};return}
func deferredRegion()(r shared[Holder],f func()int){f=func()int{return r.id};defer func(){r=fresh()}();rt.ExitTask();return}
func cleanupExit()(a,b Holder){a,b=Holder{id:100},Holder{id:1};rt.ExitTask();return}
func cleanupCountedExit()(a Holder,p shared[Holder],e error){a,p,e=Holder{id:100},freshId(200),freshSync(1);rt.ExitTask();return}
func cleanupFreeExit()(a Holder,p shared[Holder],e error){a,p,e=Holder{id:100},freshId(200),freshSync(1);rt.ExitTask();return}
func ownerCall(){v:=namedOwner();_ = v.id;panic("continued")}
func ownerDeferCall(){v:=deferredOwner();_ = v.id;panic("continued")}
func sharedCall(){v:=namedShared();_ = v;panic("continued")}
func sharedDeferCall(){v:=deferredShared();_ = v;panic("continued")}
func errorCall(){v:=namedError();_ = v;panic("continued")}
func errorDeferCall(){v:=deferredError();_ = v;panic("continued")}
func stringCall(){v:=namedString();_ = v;panic("continued")}
func stringDeferCall(){v:=deferredString();_ = v;panic("continued")}
func ownerNormalCall(){v:=normalOwner();_ = v.id}
func sharedNormalCall(){v:=normalShared();_ = v}
func ownerOverwriteCall(){v:=overwriteOwner();_ = v.id;panic("continued")}
func ownerNormalOverwriteCall(){v:=normalOverwriteOwner();_ = v.id}
func sharedOverwriteCall(){v:=overwriteShared();_ = v;panic("continued")}
func mixedCall(){r,p,e:=mixed();_ = r.id;_ = p;_ = e;panic("continued")}
func regionCall(){r,f:=region(true);_ = r;_ = f;panic("continued")}
func normalRegionCall(){r,f:=region(false);_ = r;if f()!=100{panic("bad region")}}
func deferRegionCall(){r,f:=deferredRegion();_ = r;_ = f;panic("continued")}
func ignoredCall(){namedOwner();panic("continued")}
func normalIgnoredCall(){normalOwner()}
func cleanupExitCall(){exitDrop=true;a,b:=cleanupExit();_ = a.id;_ = b.id;panic("continued")}
func cleanupCountedExitCall(){exitDrop=true;a,p,e:=cleanupCountedExit();_ = a.id;_ = p;_ = e;panic("continued")}
func cleanupFreeExitCall(){exitFree=true;a,p,e:=cleanupFreeExit();_ = a.id;_ = p;_ = e;panic("continued")}
func check(name string,f func(),wantDrops,wantFrees int,exits bool){drops=0;frees=0;exitDrop=false;exitFree=false;caught:=rt.CatchExitTask(f);println(name,caught==exits,drops==wantDrops,frees==wantFrees,drops,frees)}
func main(){heap.next=alloc.Default();alloc.SetDefault(&heap);check("owner",ownerCall,100,0,true);check("ownerDefer",ownerDeferCall,100,0,true);check("shared",sharedCall,100,1,true);check("sharedDefer",sharedDeferCall,100,1,true);check("error",errorCall,100,1,true);check("errorDefer",errorDeferCall,100,1,true);check("string",stringCall,0,1,true);check("stringDefer",stringDeferCall,0,1,true);check("normalOwner",ownerNormalCall,100,0,false);check("normalShared",sharedNormalCall,100,1,false);check("overwriteOwner",ownerOverwriteCall,101,0,true);check("normalOverwriteOwner",ownerNormalOverwriteCall,101,0,false);check("overwriteShared",sharedOverwriteCall,200,2,true);check("mixed",mixedCall,300,2,true);check("region",regionCall,100,1,true);check("normalRegion",normalRegionCall,100,1,false);check("deferredRegion",deferRegionCall,100,1,true);check("ignored",ignoredCall,100,0,true);check("normalIgnored",normalIgnoredCall,100,0,false);check("cleanupExit",cleanupExitCall,101,0,true);check("cleanupCountedExit",cleanupCountedExitCall,301,2,true);check("cleanupFreeExit",cleanupFreeExitCall,301,2,true)}
`
	const expected = "owner true true true 100 0\nownerDefer true true true 100 0\nshared true true true 100 1\nsharedDefer true true true 100 1\nerror true true true 100 1\nerrorDefer true true true 100 1\nstring true true true 0 1\nstringDefer true true true 0 1\nnormalOwner true true true 100 0\nnormalShared true true true 100 1\noverwriteOwner true true true 101 0\nnormalOverwriteOwner true true true 101 0\noverwriteShared true true true 200 2\nmixed true true true 300 2\nregion true true true 100 1\nnormalRegion true true true 100 1\ndeferredRegion true true true 100 1\nignored true true true 100 0\nnormalIgnored true true true 100 0\ncleanupExit true true true 101 0\ncleanupCountedExit true true true 301 2\ncleanupFreeExit true true true 301 2\n"
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			t.Parallel()
			exe := buildWithLib(t, "", source, optFlag(level))
			output, status := runDebugAlloc(t, exe, "")
			if status != 0 || output != expected {
				t.Fatalf("exit %d, named result cleanup:\n%s", status, output)
			}
		})
	}
}
