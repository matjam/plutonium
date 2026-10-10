package build_test

import (
	"bytes"
	"debug/elf"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// buildDebugAlloc builds src with -alloc=debug at optLevel and returns the
// executable's path.
func buildDebugAlloc(t *testing.T, src string, optLevel int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prog")
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel, Alloc: "debug"}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	return out
}

// runDebugAlloc runs prog with PLUTO_DEBUGALLOC set to settings and returns
// its output (stdout, then stderr) and exit status. A death by signal fails
// the test: the debug allocator must turn faults into messages.
func runDebugAlloc(t *testing.T, prog, settings string) (string, int) {
	t.Helper()
	cmd := exec.Command(prog)
	cmd.Env = append(os.Environ(), "PLUTO_DEBUGALLOC="+settings)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	status := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = exitErr.ExitCode()
		if status < 0 {
			t.Fatalf("program died: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
		}
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String() + stderr.String(), status
}

// debugAllocProgram is a main package whose main runs body, with p a
// 10-byte slice and n a 24-byte struct from the default allocator.
func debugAllocProgram(body string) string {
	return `package main

import (
	"pluto/alloc"
	"unsafe"
)

type node struct {
	left, right *node
	v           int
}

var global int

func main() {
	p := make([]byte, 10)
	n := alloc.New[node]()
	base := unsafe.Pointer(unsafe.SliceData(p))
	_, _, _ = p, n, base
` + body + `
}
`
}

func TestDebugAllocErrors(t *testing.T) {
	free := `
	alloc.Free(n)
	alloc.Default().Free(base, 10, 1)
`
	tests := []struct {
		name     string
		body     string
		settings string
		status   int
		want     []string // regular expressions the output must match
	}{
		{
			name: "overflow by one byte faults on the guard page",
			body: `
	*(*byte)(unsafe.Add(base, 10)) = 1
` + free,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: heap overflow: write at 0x[0-9a-f]+, offset 10 of the 10-byte allocation at 0x[0-9a-f]+ \(allocation #1\)$`,
				`(?m)^  faulting instruction at pc 0x[0-9a-f]+$`,
				`(?m)^  allocated at pc 0x[0-9a-f]+$`,
			},
		},
		{
			name: "overflow by one byte with canaries is caught at free",
			body: `
	*(*byte)(unsafe.Add(base, 10)) = 1
` + free,
			settings: "canary",
			status:   2,
			want: []string{
				`(?m)^panic: debug allocator: heap overflow: the 10-byte allocation at 0x[0-9a-f]+ \(allocation #1\) was written past its end, at offset 10; found when it was freed$`,
			},
		},
		{
			name: "underflow faults on the guard page in underflow mode",
			body: `
	println(*(*byte)(unsafe.Add(base, -1)))
` + free,
			settings: "underflow",
			status:   2,
			want: []string{
				`(?m)^panic: debug allocator: heap underflow: read at 0x[0-9a-f]+, offset -1 of the 10-byte allocation at 0x[0-9a-f]+ \(allocation #1\)$`,
			},
		},
		{
			name: "underflow with the guard page after is caught at free",
			body: `
	*(*byte)(unsafe.Add(base, -3)) = 1
` + free,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: heap underflow: the 10-byte allocation at 0x[0-9a-f]+ \(allocation #1\) was written before its start, at offset -3; found when it was freed$`,
			},
		},
		{
			name: "write after free faults",
			body: `
	alloc.Free(n)
	n.v = 7
	alloc.Default().Free(base, 10, 1)
`,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: use after free: write at 0x[0-9a-f]+, offset 16 of the 24-byte allocation at 0x[0-9a-f]+ \(allocation #2, freed\)$`,
				`(?m)^  allocated at pc 0x[0-9a-f]+$`,
				`(?m)^  freed at pc 0x[0-9a-f]+$`,
			},
		},
		{
			name: "write after free with canaries is caught by the exit check",
			body: `
	alloc.Free(n)
	n.v = 7
	alloc.Default().Free(base, 10, 1)
`,
			settings: "canary",
			status:   2,
			want: []string{
				`(?m)^panic: debug allocator: write after free: the 24-byte allocation at 0x[0-9a-f]+ \(allocation #2\) was written at offset 16 after it was freed$`,
				`(?m)^  freed at pc 0x[0-9a-f]+$`,
			},
		},
		{
			name: "write after free with canaries is caught when it leaves quarantine",
			body: `
	alloc.Free(n)
	n.v = 7
	for i := 0; i < 100; i++ {
		alloc.Free(alloc.New[node]())
	}
	println("not reached")
`,
			settings: "canary,quarantine=1K",
			status:   2,
			want: []string{
				`(?m)^panic: debug allocator: write after free: the 24-byte allocation at 0x[0-9a-f]+ \(allocation #2\) was written at offset 16 after it was freed$`,
			},
		},
		{
			name: "read after free faults",
			body: `
	alloc.Free(n)
	println(n.v)
	alloc.Default().Free(base, 10, 1)
`,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: use after free: read at 0x[0-9a-f]+, offset 16 of the 24-byte allocation at 0x[0-9a-f]+ \(allocation #2, freed\)$`,
			},
		},
		{
			name: "read after free with canaries sees poison",
			body: `
	alloc.Free(n)
	println(uint64(n.v), n.left == nil)
	alloc.Default().Free(base, 10, 1)
`,
			settings: "canary",
			status:   0,
			want: []string{
				// 0xdddddddddddddddd
				`(?m)^15987178197214944733 false$`,
			},
		},
		{
			name: "double free",
			body: `
	alloc.Free(n)
	alloc.Free(n)
`,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: double free of 0x[0-9a-f]+ \(24-byte allocation #2\)$`,
				`(?m)^  freed again at pc 0x[0-9a-f]+$`,
				`(?m)^  allocated at pc 0x[0-9a-f]+$`,
				`(?m)^  freed at pc 0x[0-9a-f]+$`,
			},
		},
		{
			name: "free with the wrong size",
			body: `
	alloc.Default().Free(base, 16, 1)
`,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: free of 0x[0-9a-f]+ with size 16, align 1: it was allocated with size 10, align 1$`,
			},
		},
		{
			name: "free with the wrong alignment",
			body: `
	alloc.Default().Free(unsafe.Pointer(n), 24, 16)
`,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: free of 0x[0-9a-f]+ with size 24, align 16: it was allocated with size 24, align 8$`,
			},
		},
		{
			name: "free of an interior pointer",
			body: `
	alloc.Default().Free(unsafe.Add(base, 4), 6, 1)
`,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: invalid free of 0x[0-9a-f]+: it points at offset 4 of the 10-byte allocation at 0x[0-9a-f]+$`,
			},
		},
		{
			name: "free of memory never allocated",
			body: `
	alloc.Default().Free(unsafe.Pointer(&global), 8, 8)
`,
			status: 2,
			want: []string{
				`(?m)^panic: debug allocator: invalid free of 0x[0-9a-f]+: it was not allocated by this allocator, or was freed long enough ago to leave quarantine$`,
			},
		},
		{
			name: "leaks are reported at exit and fail the program",
			body: `
	alloc.Free(n)
`,
			status: 2,
			want: []string{
				`(?m)^debug allocator: 1 allocation still live, 10 bytes:$`,
				`(?m)^  10 bytes at 0x[0-9a-f]+ \(allocation #1\), allocated at pc 0x[0-9a-f]+$`,
				`(?m)^debug allocator: exiting with status 2 because of leaks$`,
			},
		},
		{
			name:     "leaks=report keeps the exit status",
			body:     ``,
			settings: "leaks=report",
			status:   0,
			want: []string{
				`(?m)^debug allocator: 2 allocations still live, 34 bytes:$`,
				`(?m)^  24 bytes at 0x[0-9a-f]+ \(allocation #2\), allocated at pc 0x[0-9a-f]+$`,
			},
		},
		{
			name: "ReportLeaks on demand",
			body: `
	println(alloc.ReportLeaks())
	alloc.Free(n)
	alloc.Default().Free(base, 10, 1)
	println(alloc.ReportLeaks())
`,
			status: 0,
			want: []string{
				`(?s)debug allocator: 2 allocations still live, 34 bytes:\n.*\n2\n0\n$`,
			},
		},
		{
			name:     "bad setting",
			body:     free,
			settings: "quarantine=lots",
			status:   2,
			want: []string{
				`(?m)^panic: debug allocator: bad PLUTO_DEBUGALLOC setting "quarantine=lots"$`,
			},
		},
		{
			name: "a fault outside the heap",
			body: `
	println(*(*int)(unsafe.Pointer(uintptr(8))))
`,
			status: 2,
			want: []string{
				`(?m)^panic: runtime error: invalid memory access at 0x8 \(pc 0x[0-9a-f]+\)$`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := buildDebugAlloc(t, debugAllocProgram(tt.body), 2)
			got, status := runDebugAlloc(t, prog, tt.settings)
			if status != tt.status {
				t.Errorf("exit status %d, want %d", status, tt.status)
			}
			for _, w := range tt.want {
				if !regexp.MustCompile(w).MatchString(got) {
					t.Errorf("output does not match %q", w)
				}
			}
			if t.Failed() {
				t.Logf("output:\n%s", got)
			}
		})
	}
}

// TestDebugAllocSites checks that the allocation and free sites the debug
// allocator reports lie in the functions that allocated and freed, with and
// without inlining.
func TestDebugAllocSites(t *testing.T) {
	src := `package main

import "pluto/alloc"

type node struct{ v int }

var keep *node
var buf []byte

//pu:noinline
func makeNode() *node { return alloc.New[node]() }

//pu:noinline
func makeBuf() []byte { return make([]byte, 100) }

//pu:noinline
func release(n *node) { alloc.Free(n) }

func main() {
	keep = makeNode()
	buf = makeBuf()
	n := makeNode()
	release(n)
	release(n)
}
`
	re := regexp.MustCompile(`(?m)^  (allocated|freed again|freed) at pc 0x([0-9a-f]+)$`)
	for _, level := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(level), func(t *testing.T) {
			prog := buildDebugAlloc(t, src, level)
			got, status := runDebugAlloc(t, prog, "")
			if status != 2 || !strings.Contains(got, "panic: debug allocator: double free of ") {
				t.Fatalf("exit status %d, output:\n%s", status, got)
			}
			want := map[string]string{"allocated": "main.makeNode", "freed": "main.release", "freed again": "main.release"}
			found := 0
			for _, m := range re.FindAllStringSubmatch(got, -1) {
				pc, _ := strconv.ParseUint(m[2], 16, 64)
				fn := funcAt(t, prog, pc)
				if fn != want[m[1]] {
					t.Errorf("%s at %#x is in %s, want %s", m[1], pc, fn, want[m[1]])
				}
				found++
			}
			if found != 3 {
				t.Errorf("found %d sites, want 3; output:\n%s", found, got)
			}
		})
	}
}

// funcAt returns the name of the function symbol in prog containing pc.
func funcAt(t *testing.T, prog string, pc uint64) string {
	t.Helper()
	f, err := elf.Open(prog)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	syms, err := f.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range syms {
		if elf.ST_TYPE(s.Info) == elf.STT_FUNC && pc >= s.Value && pc < s.Value+s.Size {
			return s.Name
		}
	}
	return "?"
}

// TestLibDebugAlloc runs every testdata/lib program with the debug
// allocator as the default: the standard library must free what it
// allocates with the size and alignment it allocated, and must not touch
// memory outside its allocations.
func TestLibDebugAlloc(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "lib", "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".p")
		want, err := os.ReadFile(strings.TrimSuffix(file, ".p") + ".out")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			prog := buildDebugAlloc(t, string(src), 2)
			got, status := runDebugAlloc(t, prog, "leaks=off")
			if status != 0 {
				t.Errorf("exit status %d", status)
			}
			if got != string(want) {
				t.Errorf("output differs\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

func TestDebugAllocNeedsHosted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		alloc        string
		freestanding bool
		want         string
	}{
		{"debug", true, "-alloc=debug needs a hosted build"},
		{"fancy", false, `unknown allocator "fancy"`},
	} {
		cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib(), Freestanding: tt.freestanding, Alloc: tt.alloc}
		err := build.Build(cfg)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("-alloc=%s: error %v, want %q", tt.alloc, err, tt.want)
		}
	}
}
