package build_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestFieldNames builds a program that prints a struct with %+v and %#v
// with field names kept and stripped (--fieldnames=false): without names
// both print the fields as %v does, in Go syntax for %#v.
func TestFieldNames(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/fmt"

type point struct {
	X, Y int
	tag  string
}

func main() {
	fmt.Printf("%v|%+v|%#v\n", point{1, 2, "a"}, point{3, 4, "b"}, point{5, 6, "c"})
}
`
	cases := []struct{ setting, want string }{
		{"", `{1 2 a}|{X:3 Y:4 tag:b}|main.point{X:5, Y:6, tag:"c"}` + "\n"},
		{"on", `{1 2 a}|{X:3 Y:4 tag:b}|main.point{X:5, Y:6, tag:"c"}` + "\n"},
		{"off", `{1 2 a}|{3 4 b}|main.point{5, 6, "c"}` + "\n"},
	}
	for _, c := range cases {
		t.Run("fieldnames="+c.setting, func(t *testing.T) {
			testguard.Parallel(t)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "prog")
			cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: 2, FieldNames: c.setting}
			if err := buildCfg(cfg); err != nil {
				t.Fatalf("build: %v", err)
			}
			got, err := exec.Command(out).Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	if testCompiler != "" {
		// The command line's form of Check's test.
		if out, status := runTestCompiler(t, "build", "--fieldnames=maybe", "-o", filepath.Join(t.TempDir(), "prog"), t.TempDir()); status != 2 {
			t.Errorf("--fieldnames=maybe: status %d, output:\n%s", status, out)
		}
		return
	}
	bad := &build.Config{FieldNames: "maybe"}
	if err := bad.Check(); err == nil {
		t.Errorf("Check accepted --fieldnames=maybe")
	}
}
