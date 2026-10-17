package race_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/race"
	"github.com/matjam/plutonium/internal/testguard"
	"github.com/matjam/plutonium/internal/types2"
)

// TestScopeOverlaps checks which pairs of slices the race check leaves to
// a run-time check before each scoped task starts (language spec section
// 19.4, Memory reached through views): the pairs that tasks use in the same
// partition form and at least one of which they write.
func TestScopeOverlaps(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range []struct {
		name string
		src  string
		want []string
	}{
		{"one pair", `
import "pluto/sync"

func double(in, out []int) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i] * 2 })
	}
}

func main() { double(nil, nil) }`, []string{"in,out"}},

		{"read-only slices", `
import "pluto/sync"

func sum(a, b []int) int {
	var s sync.Scope
	var t [4]int
	for i := range t {
		s.Task(func() { t[i] = a[i] + b[i] })
	}
	s.Wait()
	return t[0]
}

func main() { println(sum(nil, nil)) }`, nil},

		{"three slices", `
import "pluto/sync"

func add(a, b, out []int) {
	var s sync.Scope
	for i := range a {
		s.Task(func() { out[i] = a[i] + b[i] })
	}
}

func main() { add(nil, nil, nil) }`, []string{"a,out", "b,out"}},

		{"other element types", `
import "pluto/sync"

func conv(in []int, out []int64) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = int64(in[i]) })
	}
}

func main() { conv(nil, nil) }`, nil},

		{"slices made here", `
import "pluto/sync"

func main() {
	in := [4]int{1, 2, 3, 4}
	var out [4]int
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i] })
	}
}`, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte("package main"+c.src), 0o644); err != nil {
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
			mv, _ := moves.Analyze(prog)
			overlaps, err := race.AnalyzeWith(prog, mv)
			if err != nil {
				t.Fatalf("unexpected error:\n%s", fullText(err))
			}
			var got []string
			for _, list := range overlaps {
				for _, o := range list {
					got = append(got, o.A.Name()+","+o.B.Name())
				}
			}
			sort.Strings(got)
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("overlap checks %v, want %v", got, c.want)
			}
		})
	}
}
