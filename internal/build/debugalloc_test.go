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
	"github.com/matjam/plutonium/internal/testguard"
)

// buildDebugAlloc builds src with --alloc=debug at optLevel and returns the
// executable's path.
func buildDebugAlloc(t *testing.T, src string, optLevel int) string {
	t.Helper()
	return buildDebugAllocWith(t, src, build.Config{OptLevel: optLevel})
}

// buildDebugAllocWith builds src with --alloc=debug and cfg's other
// settings and returns the executable's path.
func buildDebugAllocWith(t *testing.T, src string, cfg build.Config) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Dir = dir
	cfg.Output = filepath.Join(dir, "prog")
	cfg.Stdlib = plutonium.Stdlib()
	cfg.Alloc = "debug"
	if err := build.Build(&cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	return cfg.Output
}

// runDebugAlloc runs prog with PLUTO_DEBUGALLOC set to settings and returns
// its output (stdout, then stderr) and exit status, with the extra
// environment variables env. A death by signal fails the test: the debug
// allocator must turn faults into messages.
//
// A run in which main returned with tasks still running (the abandon path of
// section 15.4: the debug allocator prints a line instead of its leak report)
// is run again, because a program that joins its tasks with a WaitGroup or a
// channel can take that path when the machine is loaded (decision D31; see
// joined_test.go). The run returned is one that accounted exactly. A test
// that wants the abandon path calls runDebugAllocRaw.
func runDebugAlloc(t *testing.T, prog, settings string, env ...string) (string, int) {
	t.Helper()
	return untilRetired(t, func() (string, int) { return runDebugAllocRaw(t, prog, settings, env...) },
		func(out string, _ int) bool { return tookAbandonPath(out) })
}

// runDebugAllocRaw is runDebugAlloc without the repetition of a run that
// took the abandon path.
func runDebugAllocRaw(t *testing.T, prog, settings string, env ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(prog)
	cmd.Env = append(append(os.Environ(), "PLUTO_DEBUGALLOC="+settings), env...)
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
	p := alloc.New[[10]byte]()[:]
	n := alloc.New[node]()
	base := unsafe.Pointer(unsafe.SliceData(p))
	_, _, _ = p, n, base
` + body + `
}
`
}

func TestDebugAllocErrors(t *testing.T) {
	testguard.Parallel(t)
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
				// The whole report, in order: the allocator's
				// description, the signal, the stack trace.
				`\Apanic: debug allocator: heap overflow: write at 0x[0-9a-f]+, offset 10 of the 10-byte allocation at 0x[0-9a-f]+ \(allocation #1\)\n` +
					`  faulting instruction at ` + siteRE("main.main", 21) + `\n` +
					`  allocated at ` + siteRE("main.main", 16) + `\n` +
					`\[signal SIGSEGV: segmentation violation addr=0x[0-9a-f]+ pc=0x[0-9a-f]+\]\n` +
					`    ` + siteRE("main.main", 21) + `\n\z`,
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
				`(?m)^  faulting instruction at ` + siteRE("main.main", 22) + `$`,
				`(?m)^  allocated at ` + siteRE("main.main", 17) + `$`,
				`(?m)^  freed at ` + siteRE("main.main", 21) + `$`,
				`\n\[signal SIGSEGV: segmentation violation addr=0x[0-9a-f]+ pc=0x[0-9a-f]+\]\n    ` + siteRE("main.main", 22) + `\n\z`,
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
				`(?m)^  freed at ` + siteRE("main.main", 21) + `$`,
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
				`(?m)^  freed again at ` + siteRE("main.main", 22) + `$`,
				`(?m)^  allocated at ` + siteRE("main.main", 17) + `$`,
				`(?m)^  freed at ` + siteRE("main.main", 21) + `$`,
				// rt.Abort prints the stack trace, which ends in main.
				`\n    ` + siteRE("main.main", 22) + `\n\z`,
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
				`(?m)^  10 bytes at 0x[0-9a-f]+ \(allocation #1\), allocated at ` + siteRE("main.main", 16) + `$`,
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
				`(?m)^  24 bytes at 0x[0-9a-f]+ \(allocation #2\), allocated at ` + siteRE("main.main", 17) + `$`,
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
				// The debug allocator does not know the address, so the
				// runtime reports the fault as it does without it.
				`\Apanic: runtime error: invalid memory address or nil pointer dereference\n` +
					`\[signal SIGSEGV: segmentation violation addr=0x8 pc=0x[0-9a-f]+\]\n` +
					`    ` + siteRE("main.main", 21) + `\n\z`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testguard.Parallel(t)
			cfg := build.Config{OptLevel: 2}
			// These deliberate unsafe operations exercise the debug tool's
			// own quarantine and diagnostic contract in unchecked mode
			// (debug-allocator.md sections 1 and 5). Checked counterparts
			// are covered by the generation-directory regressions.
			if strings.Contains(tt.name, "after free") || strings.Contains(tt.name, "overflow") || strings.Contains(tt.name, "underflow") || tt.name == "double free" || tt.name == "free with the wrong size" || tt.name == "free with the wrong alignment" || tt.name == "free of an interior pointer" || tt.name == "free of memory never allocated" {
				cfg.NoGenCheck = true
			}
			prog := buildDebugAllocWith(t, debugAllocProgram(tt.body), cfg)
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

// Checked builds reject invalid tagged accesses before the debug tool's
// guard-page or canary diagnostic. Its unchecked tests above retain those
// separate diagnostic oracles.
func TestDebugAllocCheckedAccesses(t *testing.T) {
	testguard.Parallel(t)
	for _, tc := range []struct{ name, body, settings string }{
		{"overflow", `*(*byte)(unsafe.Add(base, 10)) = 1`, ""},
		{"canary overflow", `*(*byte)(unsafe.Add(base, 10)) = 1`, "canary"},
		{"underflow", `println(*(*byte)(unsafe.Add(base, -1)))`, "underflow"},
		{"canary underflow", `*(*byte)(unsafe.Add(base, -3)) = 1`, "canary"},
		{"stale", `alloc.Free(n); println(n.v)`, ""},
	} {
		for _, level := range []int{0, 2} {
			t.Run(tc.name+"/O"+strconv.Itoa(level), func(t *testing.T) {
				testguard.Parallel(t)
				prog := buildDebugAllocWith(t, debugAllocProgram(tc.body), build.Config{OptLevel: level, GenCheck: true})
				out, status := runDebugAlloc(t, prog, tc.settings)
				if status != 2 || !strings.HasPrefix(out, "panic: runtime error: use of freed memory:") {
					t.Fatalf("want checked access panic: status %d, output %q", status, out)
				}
			})
		}
	}
}

// siteRE returns a regular expression matching how the debug allocator
// names a site in function fn at line of main.p.
func siteRE(fn string, line int) string {
	return regexp.QuoteMeta(fn) + ` \(\S*/main\.p:` + strconv.Itoa(line) + `\)`
}

// debugAllocSitesProgram allocates in functions of its own, some kept out
// of line and makeSmall inlined at -O2, leaves three allocations live and
// frees one; free, if set, frees it a second time on line 28.
func debugAllocSitesProgram(free bool) string {
	tail := ""
	if free {
		tail = "\trelease(n)\n"
	}
	return `package main

import "pluto/alloc"

type node struct{ v int }

var keep *node
var buf []byte
var small []byte

//pu:noinline
func makeNode() *node { return alloc.New[node]() }

//pu:noinline
func makeBuf() []byte { return alloc.New[[100]byte]()[:] }

//pu:noinline
func release(n *node) { alloc.Free(n) }

func makeSmall() []byte { return alloc.New[[50]byte]()[:] }

func main() {
	keep = makeNode()
	buf = makeBuf()
	small = makeSmall()
	n := makeNode()
	release(n)
` + tail + `}
`
}

// TestDebugAllocSites checks that the debug allocator names the function
// and line that allocated or freed, at -O0 and -O2, in a leak report and a
// double-free report: the sites rt.CallerPC records symbolise through the
// trace table to the call of new, make, alloc.New or alloc.Free, including
// where that call was inlined.
func TestDebugAllocSites(t *testing.T) {
	testguard.Parallel(t)
	for _, level := range []int{0, 2} {
		t.Run("leaks/O"+strconv.Itoa(level), func(t *testing.T) {
			testguard.Parallel(t)
			prog := buildDebugAlloc(t, debugAllocSitesProgram(false), level)
			got, status := runDebugAlloc(t, prog, "")
			want := `\Adebug allocator: 3 allocations still live, 158 bytes:\n` +
				`  8 bytes at 0x[0-9a-f]+ \(allocation #1\), allocated at ` + siteRE("main.makeNode", 12) + `\n` +
				`  100 bytes at 0x[0-9a-f]+ \(allocation #2\), allocated at ` + siteRE("main.makeBuf", 15) + `\n` +
				`  50 bytes at 0x[0-9a-f]+ \(allocation #3\), allocated at ` + siteRE("main.makeSmall", 20) + `\n` +
				`debug allocator: exiting with status 2 because of leaks\n\z`
			if status != 2 || !regexp.MustCompile(want).MatchString(got) {
				t.Errorf("exit status %d, output:\n%s", status, got)
			}
		})
		t.Run("double free/O"+strconv.Itoa(level), func(t *testing.T) {
			testguard.Parallel(t)
			var listing bytes.Buffer
			prog := buildDebugAllocWith(t, debugAllocSitesProgram(true), build.Config{OptLevel: level, Listing: &listing, NoGenCheck: true})
			got, status := runDebugAlloc(t, prog, "")
			want := `\Apanic: debug allocator: double free of 0x[0-9a-f]+ \(8-byte allocation #4\)\n` +
				`  freed again at ` + siteRE("main.release", 18) + `\n` +
				`  allocated at ` + siteRE("main.makeNode", 12) + `\n` +
				`  freed at ` + siteRE("main.release", 18) + `\n` +
				`(?:    .*\n)*` +
				`    ` + siteRE("main.release", 18) + `\n` +
				`    ` + siteRE("main.main", 28) + `\n\z`
			if status != 2 || !regexp.MustCompile(want).MatchString(got) {
				t.Errorf("exit status %d, output:\n%s", status, got)
			}
			if level == 2 {
				// alloc.Free is inlined into release, where its
				// rt.CallerPC becomes the address of the inlined code;
				// "freed at" above shows it still names release's line.
				_, release, _ := strings.Cut(listing.String(), "\nmain.release:\n")
				release, _, _ = strings.Cut(release, "\n\n")
				if !strings.Contains(release, "0(%rip), %") {
					t.Errorf("alloc.Free's CallerPC is not inlined into main.release; the test no longer covers inlined sites")
				}
			}
		})
	}
}

// TestDebugAllocSitesUnsymbolised checks the sites a program built with
// --trace=false reports, which has no table to name them: PrintPC falls
// back to the address of the call, which must lie in the function that
// allocated or freed.
func TestDebugAllocSitesUnsymbolised(t *testing.T) {
	testguard.Parallel(t)
	re := regexp.MustCompile(`(?m)^  (allocated|freed again|freed) at pc 0x([0-9a-f]+)$`)
	for _, level := range []int{0, 2} {
		t.Run("O"+strconv.Itoa(level), func(t *testing.T) {
			testguard.Parallel(t)
			prog := buildDebugAllocWith(t, debugAllocSitesProgram(true), build.Config{OptLevel: level, NoTrace: true, NoGenCheck: true})
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

// TestCallerPC checks the code for rt.CallerPC: a load of the return
// address in a function of its own, the address of the code where it is
// inlined, and the interrupted RIP in an interrupt handler.
func TestCallerPC(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import (
	"pluto/arch/x86"
	"pluto/rt"
	"pluto/sync/atomic"
)

var last atomic.Uintptr

//pu:noinline
func where() uintptr { return rt.CallerPC() }

func here() uintptr { return rt.CallerPC() }

//pu:interrupt
func handler(f *x86.InterruptFrame) { last.Store(rt.CallerPC()) }

func main() {
	a, b := where(), where()
	c, d := here(), here()
	gate := x86.InterruptGate(handler, 0x08, 0)
	println(a != 0, a != b, c != 0, c != d, gate != x86.Gate{})
}
`
	for _, level := range []int{0, 2} {
		got, status := compileAndRun(t, src, level)
		if status != 0 || got != "true true true true true\n" {
			t.Errorf("-O%d: status %d, output %q", level, status, got)
		}
	}
	if where := listFunc(t, src, "main.where"); !strings.Contains(where, "(%rsp), %") {
		t.Errorf("where does not load its return address:\n%s", where)
	}
	if main := listFunc(t, src, "main.main"); !strings.Contains(main, "0(%rip), %") {
		t.Errorf("inlined CallerPC does not take the code address:\n%s", main)
	}
	if handler := listFunc(t, src, "main.handler"); !strings.Contains(handler, "(%rsp), %") {
		t.Errorf("handler does not load the interrupted RIP:\n%s", handler)
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
	testguard.Parallel(t)
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
			testguard.Parallel(t)
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
	testguard.Parallel(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		alloc                string
		freestanding, kernel bool
		want                 string
	}{
		{"debug", true, false, "--alloc=debug needs a hosted build"},
		{"debug", false, true, "--alloc=debug needs a hosted build"},
		{"fancy", false, false, `unknown allocator "fancy"`},
	} {
		cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib(), Freestanding: tt.freestanding, Kernel: tt.kernel, Alloc: tt.alloc}
		err := build.Build(cfg)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("-alloc=%s: error %v, want %q", tt.alloc, err, tt.want)
		}
	}
}
