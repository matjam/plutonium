package build_test

import (
	"context"
	"errors"
	"fmt"
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

// Selected hosted hooks exit once before callback handoff, after callback
// handoff, or after a new runner has parked. Promised callbacks run once;
// an aborted timer constructor must not strand the published runner.
func TestNormalShutdownTimerHandoff(t *testing.T) {
	testguard.Parallel(t)
	lib := plutonium.Stdlib()
	data, err := fs.ReadFile(lib, "pluto/rt/sched_linux.p")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	const newTask = "func (*hostedSched) NewTask(req StackRequest) *Task {"
	const spawn = "func (*hostedSched) Spawn(t *Task) { hostedReady(t) }"
	if !strings.Contains(text, newTask) || !strings.Contains(text, spawn) {
		t.Fatal("missing selected hosted scheduler hooks")
	}
	text = strings.Replace(text, newTask, newTask+`
	if xadd32(&d6TimerCreates,1)==2 && load32(&d6TimerMode)==1 {
		store32(&d6TimerMode,0)
		ExitTask()
		return nil
	}`, 1)
	text = strings.Replace(text, spawn, `func (*hostedSched) Spawn(t *Task) {
	sequence:=xadd32(&d6TimerSpawns,1)
	hostedReady(t)
	mode:=load32(&d6TimerMode)
	if sequence==2 && mode==2 {
		store32(&d6TimerMode,0)
		ExitTask()
	}
	if sequence==1 && mode==3 {
		for load32(&t.wake)!=wakeParked { Yield() }
		store32(&d6TimerMode,0)
		ExitTask()
	}
}`, 1)
	text += `
var d6TimerMode uint32
var d6TimerCreates uint32
var d6TimerSpawns uint32
func D6TimerHook(mode uint32){store32(&d6TimerMode,mode)}
`
	stdlib := goexitHookOverlay{lib, fstest.MapFS{"pluto/rt/sched_linux.p": &fstest.MapFile{Data: []byte(text)}}}
	for _, tc := range []struct {
		name string
		mode int
		main string
		want string
	}{
		{"normal", 0, `kept=time.AfterFunc(time.Millisecond,fire);time.Sleep(50*time.Millisecond)`, "fired 1\n"},
		{"beforeHandoff", 1, `kept=time.AfterFunc(time.Millisecond,fire);time.Sleep(50*time.Millisecond)`, "fired 1\n"},
		{"afterHandoff", 2, `kept=time.AfterFunc(time.Millisecond,fire);time.Sleep(50*time.Millisecond)`, "fired 1\n"},
		{"runnerRollback", 3, `println("caught",rt.CatchExitTask(create))`, "caught true\nfired 0\n"},
	} {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", tc.name, opt), func(t *testing.T) {
				testguard.Parallel(t)
				source := `package main
import "pluto/rt"
import "pluto/time"
import "pluto/sync/atomic"
var fired int32
func fire(){atomic.AddInt32(&fired,1)}
type observer owner struct{live bool}
func(o *observer)Drop(){if o.live{println("fired",atomic.LoadInt32(&fired));o.live=false}}
var last=observer{live:true}
var kept synced[time.Timer]
func create(){t:=time.AfterFunc(time.Hour,fire);_ = t}
func main(){rt.D6TimerHook(MODE);MAIN}
`
				source = strings.ReplaceAll(source, "MODE", fmt.Sprint(tc.mode))
				source = strings.ReplaceAll(source, "MAIN", tc.main)
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(source), 0o644); err != nil {
					t.Fatal(err)
				}
				cfg := build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: stdlib, OptLevel: opt, GenCheck: true}
				if err := build.Build(&cfg); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				out, err := exec.CommandContext(ctx, cfg.Output).CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("runner did not retire: %v; output %q", ctx.Err(), out)
				}
				var exited *exec.ExitError
				if errors.As(err, &exited) {
					t.Fatalf("exit %d, output %q; want %q", exited.ExitCode(), out, tc.want)
				}
				if err != nil || string(out) != tc.want {
					t.Fatalf("error %v, output %q; want %q", err, out, tc.want)
				}
			})
		}
	}
}
