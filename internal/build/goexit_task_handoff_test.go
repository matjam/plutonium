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

// Creation exits before the argument record is handed to a new task.
func TestGoexitTaskHandoff(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/alloc"
import "pluto/rt"
import "unsafe"
type backend struct{}
//pu:scheduler
var scheduler backend
func(*backend)NewTask(rt.StackRequest)*rt.Task{println("creation exit");rt.Goexit();return nil}
func(*backend)FreeTask(*rt.Task){}
func(*backend)Spawn(*rt.Task){}
func(*backend)Park(*rt.Task){}
func(*backend)Wake(*rt.Task){}
func(*backend)Yield(*rt.Task){}
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("drop",p.id)}}
func(p *item)Use(){}
func owner()owned[item]{p:=alloc.Owned[item]();p.id=1;return p}
func counted()synced[item]{p:=alloc.Synced[item]();p.id=2;return p}
func useOwner(p owned[item]){println("unexpected owner",p.id)}
func useCounted(p synced[item]){println("unexpected counted",p.id)}
func createOwner(){task useOwner(owner());panic("continued owner")}
func createCounted(){task useCounted(counted());panic("continued counted")}
func createLocal(){p:=item{id:3};task useItem(p);panic("continued local")}
func createLiteral(){task useItem(item{id:4});panic("continued literal")}
func useItem(p item){println("unexpected item",p.id)}
func createReceiver(){task owner().Use();panic("continued receiver")}
type counter struct{next alloc.Allocator}
var heap counter
func(c *counter)Alloc(n,a uintptr)unsafe.Pointer{return c.next.Alloc(n,a)}
func(c *counter)Free(p unsafe.Pointer,n,a uintptr){c.next.Free(p,n,a);println("string free")}
func text()string{p,ctl:=rt.NewStringBuffer(3,&heap);copy(unsafe.Slice(p,3),[]byte{'a','b','c'});s:=rt.MakeString(p,3,ctl,true);rt.ReleaseString(ctl);return s}
func useString(s string){println("unexpected string",s)}
func createString(){task useString(text());panic("continued string")}
func main(){heap.next=alloc.Default();println("owner exit",rt.CatchGoexit(createOwner));println("counted exit",rt.CatchGoexit(createCounted));println("local exit",rt.CatchGoexit(createLocal));println("literal exit",rt.CatchGoexit(createLiteral));println("receiver exit",rt.CatchGoexit(createReceiver));println("string exit",rt.CatchGoexit(createString))}
`
	const expected = "creation exit\ndrop 1\nowner exit true\ncreation exit\ndrop 2\ncounted exit true\ncreation exit\ndrop 3\nlocal exit true\ncreation exit\ndrop 4\nliteral exit true\ncreation exit\ndrop 1\nreceiver exit true\ncreation exit\nstring free\nstring exit true\n"
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(source), 0644); err != nil {
				t.Fatal(err)
			}
			cfg := build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), OptLevel: level, Stdlib: plutonium.Stdlib()}
			if err := build.Build(&cfg); err != nil {
				t.Fatal(err)
			}
			output, status := runDebugAlloc(t, cfg.Output, "")
			if status != 0 || output != expected {
				t.Fatalf("exit %d, task handoff cleanup:\n%s", status, output)
			}
		})
	}
}

// A backend has accepted the record before its Spawn callback exits.
func TestGoexitTaskPublishedHandoff(t *testing.T) {
	testguard.Parallel(t)
	lib := plutonium.Stdlib()
	data, err := fs.ReadFile(lib, "pluto/rt/sched_linux.p")
	if err != nil {
		t.Fatal(err)
	}
	original := "func (*hostedSched) Spawn(t *Task) { hostedReady(t) }"
	modified := "func (*hostedSched) Spawn(t *Task) { hostedReady(t); if item96SpawnExit { item96SpawnExit=false; Goexit() } }"
	if !strings.Contains(string(data), original) {
		t.Fatal("missing original Spawn binding")
	}
	data = []byte(strings.Replace(string(data), original, modified, 1) + "\nvar item96SpawnExit bool\nfunc Item96ArmSpawnExit(enabled bool){item96SpawnExit=enabled}\n")
	stdlib := goexitHookOverlay{lib, fstest.MapFS{"pluto/rt/sched_linux.p": &fstest.MapFile{Data: data}}}
	const source = `package main
import "pluto/alloc"
import "pluto/rt"
import "pluto/sync"
import "pluto/sync/atomic"
import "unsafe"
var released sync.WaitGroup
var drops int32
var stringFrees int32
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{atomic.AddInt32(&drops,int32(p.id));released.Done()}}
func(p *item)Use(){}
func owner(id int)owned[item]{p:=alloc.Owned[item]();p.id=id;return p}
func counted()synced[item]{p:=alloc.Synced[item]();p.id=3;return p}
func useOwner(p owned[item]){_ = p.id}
func useCounted(p synced[item]){_ = p.id}
func createReceiver(){task owner(1).Use()}
func createOwner(){task useOwner(owner(2))}
func createCounted(){task useCounted(counted())}
type counter struct{next alloc.Allocator}
var stringsHeap counter
func(c *counter)Alloc(n,a uintptr)unsafe.Pointer{return c.next.Alloc(n,a)}
func(c *counter)Free(p unsafe.Pointer,n,a uintptr){c.next.Free(p,n,a);atomic.AddInt32(&stringFrees,1);released.Done()}
func text()string{p,ctl:=rt.NewStringBuffer(3,&stringsHeap);copy(unsafe.Slice(p,3),[]byte{'a','b','c'});s:=rt.MakeString(p,3,ctl,true);rt.ReleaseString(ctl);return s}
func useString(s string){if s!="abc"{panic("wrong string")}}
func createString(){task useString(text())}
func check(name string,f func(),want int32){released.Add(1);rt.Item96ArmSpawnExit(MODE);caught:=rt.CatchGoexit(f);released.Wait();println(name,caught==MODE,atomic.LoadInt32(&drops)==want,atomic.LoadInt32(&stringFrees))}
func main(){stringsHeap.next=alloc.Default();check("receiver",createReceiver,1);check("owner",createOwner,3);check("counted",createCounted,6);check("string",createString,6)}
`
	const expected = "receiver true true 0\nowner true true 0\ncounted true true 0\nstring true true 1\n"
	for _, mode := range []string{"false", "true"} {
		for _, level := range []int{0, 2} {
			t.Run(fmt.Sprintf("exit%s/O%d", mode, level), func(t *testing.T) {
				testguard.Parallel(t)
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(strings.ReplaceAll(source, "MODE", mode)), 0644); err != nil {
					t.Fatal(err)
				}
				cfg := build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), OptLevel: level, Stdlib: stdlib}
				if err := build.Build(&cfg); err != nil {
					t.Fatal(err)
				}
				output, status := runDebugAlloc(t, cfg.Output, "")
				if status != 0 || output != expected {
					t.Fatalf("exit %d, published task handoff:\n%s", status, output)
				}
			})
		}
	}
}
