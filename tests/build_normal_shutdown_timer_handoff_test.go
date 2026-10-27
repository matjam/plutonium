// Ported from internal/build/normal_shutdown_timer_handoff_test.go at the go-cutover tag.

package tests

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Selected hosted hooks exit once before callback handoff, after callback
// handoff, or after a new runner has parked. Promised callbacks run once;
// an aborted timer constructor must not strand the published runner.
func TestNormalShutdownTimerHandoff(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(libRoot(t), "pluto", "rt", "sched_linux.p"))
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
	stdlib := libWithPatchedFile(t, "pluto/rt/sched_linux.p", text)
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
				t.Parallel()
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
				dir := writeTree(t, single(source))
				exe := filepath.Join(t.TempDir(), "prog")
				out, status := runPluto(t, dir, []string{"PLUTOROOT=" + stdlib}, "build", optFlag(opt), "--gencheck", "-o", exe, ".")
				if status != 0 {
					t.Fatalf("build: exit status %d\n%s", status, out)
				}
				got, status := runWithTimeout(t, 10*time.Second, exe)
				if status != 0 || got != tc.want {
					t.Fatalf("exit %d, output %q; want %q", status, got, tc.want)
				}
			})
		}
	}
}

// libWithPatchedFile copies the standard library to a new directory, with
// the file at the slash-separated path name replaced by text, and returns the
// directory, which a build takes as $PLUTOROOT.
func libWithPatchedFile(t *testing.T, name, text string) string {
	t.Helper()
	root := libRoot(t)
	dst := t.TempDir()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == name {
			data = []byte(text)
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}
