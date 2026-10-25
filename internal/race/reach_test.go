package race

import (
	"os"
	"path/filepath"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// TestInterfaceCallReach checks that a call through an interface makes
// reachable only the methods of its name with as many parameters and
// results (methodCandidates). (*spawner).Reset(int) cannot be the Reset()
// that main calls, so the program cannot start a task and is not analysed.
// The whole compiler once was, because its calls of Arena.Reset() reached
// (*time.Timer).Reset(Duration) and with it time's AfterFunc runner.
func TestInterfaceCallReach(t *testing.T) {
	for _, c := range []struct {
		name   string
		call   string
		spawns bool
	}{
		{"other arity", "r.Reset()", false},
		{"same arity", "r.Reset(1)", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			sig := "Reset()"
			if c.spawns {
				sig = "Reset(k int)"
			}
			src := `package main

var n int

type resetter interface{ ` + sig + ` }

type plain struct{}

func (plain) ` + sig + ` {}

type spawner struct{}

func (*spawner) Reset(k int) {
	task func() { n++ }()
	n++
}

func main() {
	var r resetter = plain{}
	` + c.call + `
}
`
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			prog, err := load.Load(&load.Config{
				Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
				Stdlib: plutonium.Stdlib(),
				Sizes:  types2.SizesFor("gc", "amd64"),
			}, dir)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			a := newAnalyzer(prog)
			a.callGraph()
			if got := a.mayStartTasks(); got != c.spawns {
				t.Errorf("mayStartTasks = %v, want %v", got, c.spawns)
			}
		})
	}
}
