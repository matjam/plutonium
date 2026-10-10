package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// runGenCheck builds src with the generational-reference prototype at the
// given optimisation level and returns its output and exit status.
func runGenCheck(t *testing.T, src string, optLevel int) (string, int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prog")
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel, GenCheck: true}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	var stdout bytes.Buffer
	cmd := exec.Command(out)
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), 0
}

// TestGenCheck checks that, built with -gencheck, a use of freed memory and
// a double free trap, while correct code runs as before.
func TestGenCheck(t *testing.T) {
	cases := []struct {
		name, body, want string
		status           int
	}{
		{"use after free", `
	a := alloc.New[Node]()
	a.val = 7
	b := a
	alloc.Free(a)
	c := alloc.New[Node]() // reuses the slot
	c.val = 9
	println("stale", b.val)`, "use of freed memory", 2},
		{"interior pointer after free", `
	a := alloc.New[Node]()
	p := &a.val
	alloc.Free(a)
	_ = alloc.New[Node]()
	*p = 1`, "use of freed memory", 2},
		{"double free", `
	a := alloc.New[Node]()
	alloc.Free(a)
	alloc.Free(a)`, "already freed", 2},
		{"correct code", `
	var head *Node
	for i := 0; i < 1000; i++ {
		n := alloc.New[Node]()
		n.val = i
		n.next = head
		head = n
	}
	sum := 0
	for n := head; n != nil; {
		sum += n.val
		next := n.next
		alloc.Free(n)
		n = next
	}
	println("sum", sum)`, "sum 499500", 0},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				src := "package main\n\nimport \"pluto/alloc\"\n\ntype Node struct {\n\tnext *Node\n\tval  int\n}\n\nfunc main() {" + c.body + "\n}\n"
				out, status := runGenCheck(t, src, opt)
				if status != c.status || !strings.Contains(out, c.want) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, c.want)
				}
			})
		}
	}
}

func TestGenCheckFreestanding(t *testing.T) {
	cfg := &build.Config{Dir: t.TempDir(), Freestanding: true, GenCheck: true, Stdlib: plutonium.Stdlib()}
	if err := os.WriteFile(filepath.Join(cfg.Dir, "main.p"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := build.Build(cfg)
	if err == nil || !strings.Contains(err.Error(), "hosted builds only") {
		t.Fatalf("error %v, want hosted builds only", err)
	}
}
