package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// Several tests run the same translated demo: TestDifferential runs every
// demo at -O0 and -O2, and TestParseCheck, TestTypeCheck, TestLower and
// TestMixedPipelineChecked run parsecheck, typecheck, lowerdemo and ssaback
// over a corpus. demoBinary builds each (demo, level, checks) once per test
// binary and hands every test the same executable, so the translated type
// checker (4 GB to build) and front end (15 GB) are built once per level,
// not twice. goBinary does the same for the Go programs the tests compare
// with.

// demoKey names one build of a demo.
type demoKey struct {
	name       string
	level      int
	noGenCheck bool
}

// demoBuild is a build, done once.
type demoBuild struct {
	once sync.Once
	bin  string
	err  error
}

var (
	demoMu     sync.Mutex
	demoBuilds = map[demoKey]*demoBuild{}
	// demoDir holds the executables until the test binary exits.
	demoDir string
)

// demoPeakMB is the peak memory of building a demo, in MB, at -O0 and -O2,
// measured (pluto build alone, GOGC=50 as bigjob runs the tests) and
// rounded up; a build not listed fits in a parallel slot (internal/
// testguard). The build reserves this much of the run's memory budget
// (testguard.Reserve), so the guard does not start two builds of the
// translated type checker in an 11 GB cgroup. lowerdemo's 15 GB is more
// than the budget there, so it builds alone. The figures are in
// docs/verification/2026-10-21-test-memory.md.
var demoPeakMB = map[string][2]uint64{
	"ssademo":    {220, 300},
	"amd64demo":  {320, 520},
	"optdemo":    {480, 850},
	"ssaback":    {560, 1000},
	"parsecheck": {280, 390},
	"diagdemo":   {3900, 4000},
	"typecheck":  {4200, 4200},
	"lowerdemo":  {15360, 15360},
}

// demoRunMB is the peak memory of running a demo in TestDifferential, in MB,
// where it is more than a slot; the run reserves it.
var demoRunMB = map[string]uint64{
	"constdemo": 1300,
}

// demoBinary returns the executable of selfhost/demo/<name> built at the
// level, unchecked if noGenCheck, building it on the first call.
func demoBinary(t *testing.T, name string, level int, noGenCheck bool) string {
	t.Helper()
	k := demoKey{name, level, noGenCheck}
	demoMu.Lock()
	b := demoBuilds[k]
	if b == nil {
		b = &demoBuild{}
		demoBuilds[k] = b
	}
	demoMu.Unlock()
	b.once.Do(func() { b.bin, b.err = buildDemo(t, k) })
	if b.err != nil {
		t.Fatalf("building %s at -O%d: %v", name, level, b.err)
	}
	return b.bin
}

// buildDemo builds the demo of k into demoDir, or takes it from the demo
// cache (democache_test.go).
func buildDemo(t *testing.T, k demoKey) (string, error) {
	cache, base := demoCache()
	var key string
	if cache != "" {
		key = cacheKey(base, k)
		if bin, ok := cacheGet(cache, key); ok {
			t.Logf("%s at -O%d: from the demo cache (PLUTO_TEST_CACHE=0 builds it)", k.name, k.level)
			return bin, nil
		}
	}
	bin, err := compileDemo(t, k)
	if err == nil && cache != "" {
		if err := cachePut(cache, key, bin); err != nil {
			t.Logf("demo cache: %v", err)
		}
	}
	return bin, err
}

// compileDemo builds the demo of k into demoDir.
func compileDemo(t *testing.T, k demoKey) (string, error) {
	if peak, ok := demoPeakMB[k.name]; ok {
		release := testguard.Reserve(t, peak[min(k.level, 1)])
		defer release()
	}
	bin := filepath.Join(demoDir, fmt.Sprintf("%s-O%d", k.name, k.level))
	if k.noGenCheck {
		bin += "-nogencheck"
	}
	cfg := &build.Config{Dir: filepath.Join(selfhostDir, "demo", k.name), Output: bin,
		Stdlib: plutonium.Stdlib(), OptLevel: k.level, NoGenCheck: k.noGenCheck}
	return bin, build.Build(cfg)
}

var (
	goMu     sync.Mutex
	goBuilds = map[string]*demoBuild{}
	// goBuildMu lets one go build run at a time. Each runs as many
	// compiles as there are CPUs, and builds started together compile the
	// same packages (the compiler's, which most demos import) side by
	// side, each in its own process, before any of them reaches the build
	// cache; one after another, the later ones find them there.
	goBuildMu sync.Mutex
)

// goBinary returns the executable of the Go main package pkg (relative to
// this directory), built with go build on the first call.
func goBinary(t *testing.T, pkg string) string {
	t.Helper()
	goMu.Lock()
	b := goBuilds[pkg]
	if b == nil {
		b = &demoBuild{}
		goBuilds[pkg] = b
	}
	goMu.Unlock()
	b.once.Do(func() {
		goBuildMu.Lock()
		defer goBuildMu.Unlock()
		b.bin = filepath.Join(demoDir, "go-"+strings.NewReplacer("/", "_", ".", "_").Replace(pkg))
		if out, err := exec.Command("go", "build", "-o", b.bin, pkg).CombinedOutput(); err != nil {
			b.err = fmt.Errorf("go build %s: %v\n%s", pkg, err, out)
		}
	})
	if b.err != nil {
		t.Fatal(b.err)
	}
	return b.bin
}

// makeDemoDir makes demoDir; the function it returns removes it. The
// directory's name holds the process ID, and makeDemoDir first removes the
// directories of test binaries that have exited without removing theirs
// (stopped by testguard or killed): /tmp is often a tmpfs, so they would
// hold memory.
func makeDemoDir() (cleanup func(), err error) {
	removeStaleDemoDirs(os.TempDir())
	demoDir, err = os.MkdirTemp("", fmt.Sprintf("go2pu-demos-%d-", os.Getpid()))
	if err != nil {
		return nil, err
	}
	return func() { _ = os.RemoveAll(demoDir) }, nil
}

// removeStaleDemoDirs removes the demo directories under tmp whose process
// no longer runs.
func removeStaleDemoDirs(tmp string) {
	dirs, _ := filepath.Glob(filepath.Join(tmp, "go2pu-demos-*-*"))
	for _, d := range dirs {
		pidText, _, _ := strings.Cut(strings.TrimPrefix(filepath.Base(d), "go2pu-demos-"), "-")
		pid, err := strconv.Atoi(pidText)
		if err != nil || pid <= 0 {
			continue
		}
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			_ = os.RemoveAll(d)
		}
	}
}

// TestRemoveStaleDemoDirs checks that the demo directory of a process that
// has exited is removed, and that of a running one kept.
func TestRemoveStaleDemoDirs(t *testing.T) {
	testguard.Parallel(t)
	exited := exec.Command(os.Args[0], "-test.run=^$")
	if err := exited.Run(); err != nil {
		t.Skip(err)
	}
	tmp := t.TempDir()
	dead := filepath.Join(tmp, fmt.Sprintf("go2pu-demos-%d-1", exited.Process.Pid))
	live := filepath.Join(tmp, fmt.Sprintf("go2pu-demos-%d-2", os.Getpid()))
	other := filepath.Join(tmp, "go2pu-demos-x-3")
	for _, d := range []string{dead, live, other} {
		if err := os.MkdirAll(filepath.Join(d, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removeStaleDemoDirs(tmp)
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("the directory of an exited process is still there: %v", err)
	}
	for _, d := range []string{live, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was removed: %v", d, err)
		}
	}
}
