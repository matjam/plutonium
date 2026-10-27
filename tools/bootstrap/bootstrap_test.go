package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const repoRoot = "../.."

// TestCorpus checks the list of programs the stages are compared on.
func TestCorpus(t *testing.T) {
	progs, err := corpus(repoRoot, 3)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, p := range progs {
		kind, _, _ := strings.Cut(p.name, "/")
		kinds[kind]++
		if p.diag && p.file != "" {
			t.Errorf("%s: a diagnostics program is a directory", p.name)
		}
	}
	for _, k := range []string{"examples", "run", "lib", "trace", "kernel", "diag"} {
		if kinds[k] == 0 {
			t.Errorf("no %s programs in the corpus: %v", k, kinds)
		}
	}
}

func TestFirstDifference(t *testing.T) {
	if got := firstDifference([]byte("a\nb\n"), []byte("a\nc\n")); !strings.Contains(got, "line 2") {
		t.Errorf("firstDifference: %s", got)
	}
	if got := firstDifference([]byte("a"), []byte("a")); !strings.Contains(got, "same") {
		t.Errorf("firstDifference of equal texts: %s", got)
	}
}

// TestStage1 runs stage 1 of the bootstrap (the Go-hosted compiler builds
// compiler/cmd/pluto into pluto1) and checks pluto1 against the Go-hosted
// compiler over the corpus: the same executables, diagnostics and exit
// statuses at -O0 and -O2. Building pluto1 takes about 20 minutes and 20
// GB, and the corpus hours of CPU time, so it runs only with GO2PU_SLOW=1;
// BOOTSTRAP_ONLY=<substring> narrows the corpus.
func TestStage1(t *testing.T) {
	if os.Getenv("GO2PU_SLOW") == "" {
		t.Skip("builds the self-hosted compiler (set GO2PU_SLOW=1)")
	}
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	o := &options{stages: 1, corpus: true, work: t.TempDir(), limitMB: 24000, flags: []string{"-O2"},
		libEvery: 3, jobs: 6, only: os.Getenv("BOOTSTRAP_ONLY")}
	if err := run(o); err != nil {
		t.Fatal(err)
	}
}
