package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDeterministic(t *testing.T) {
	for _, seed := range []int64{1, 2, 99, 12345} {
		a := Generate(seed, Options{})
		b := Generate(seed, Options{})
		if a != b {
			t.Fatalf("seed %d: two programs differ", seed)
		}
	}
	if Generate(1, Options{}) == Generate(2, Options{}) {
		t.Fatal("seeds 1 and 2 give the same program")
	}
}

// TestGenerateTypeChecks checks that generated programs are valid Go.
func TestGenerateTypeChecks(t *testing.T) {
	n := int64(300)
	if testing.Short() {
		n = 50
	}
	for seed := int64(1); seed <= n; seed++ {
		for _, opt := range []Options{{}, {RangeWrites: true}} {
			src := Generate(seed, opt)
			errs, err := typeErrors(src)
			if err != nil {
				t.Fatalf("seed %d: parse: %v", seed, err)
			}
			if len(errs) > 0 {
				t.Fatalf("seed %d: %v\n%s", seed, errs[0], numbered(src))
			}
		}
	}
}

// TestGenerateRuns builds and runs a few programs with Go: they must exit
// cleanly, which means no index is out of range and nothing divides by
// zero.
func TestGenerateRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the Go toolchain")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	r := &runner{work: t.TempDir(), runLimit: runTimeout}
	for seed := int64(1); seed <= 5; seed++ {
		dir := filepath.Join(r.work, "p")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		o := r.runGo(dir, Generate(seed, Options{}))
		if o.class != "ok" || o.status != 0 {
			t.Fatalf("seed %d: %s\n%s", seed, o, tail(o.output, 20))
		}
		if !strings.Contains(o.output, "main ") {
			t.Fatalf("seed %d: main did not finish:\n%s", seed, tail(o.output, 5))
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReduceHelpers(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tx := 1\n\ty := 2\n\tprintln(x + y)\n}\n"
	items := removable(mustParse(src))
	if len(items) != 3 {
		t.Fatalf("got %d removable items, want 3", len(items))
	}
	// Removing the println leaves x and y unused; repair uses them.
	out, ok := repair(removeItems(src, map[int]bool{2: true}))
	if !ok || !strings.Contains(out, "_ = x") || !strings.Contains(out, "_ = y") {
		t.Fatalf("repair failed (%v):\n%s", ok, out)
	}
	if len(mutations(src)) == 0 {
		t.Fatal("no mutations")
	}
}

func numbered(src string) string {
	var b strings.Builder
	for i, l := range strings.Split(src, "\n") {
		b.WriteString(strings.Repeat(" ", 4-len(itoa(i+1))) + itoa(i+1) + "  " + l + "\n")
	}
	return b.String()
}

func itoa(i int) string {
	s := ""
	for {
		s = string(rune('0'+i%10)) + s
		i /= 10
		if i == 0 {
			return s
		}
	}
}
