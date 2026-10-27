// Command bootstrap runs the stages of the self-hosting plan (section 6.1)
// and checks them:
//
//   - stage 0: a compiler to start from: -pluto0 FILE; by default the
//     release compiler (releaseTag), downloaded from GitHub and checked
//     against its SHA-256; with -pluto0 go-cutover, the Go-hosted compiler
//     of that tag, checked out into a temporary worktree and built there
//     with go build (the Go sources are gone from the current tree);
//   - stage 1: stage 0 builds compiler/cmd/pluto, the driver and compiler
//     translated to Plutonium, into pluto1;
//   - with -corpus, pluto1 and stage 0 build the programs of the test corpus
//     (corpus.go) at -O0 and -O2, and every executable (or object file),
//     diagnostic and exit status must be byte-identical;
//   - stage 2: pluto1 builds compiler/cmd/pluto into pluto2;
//   - stage 3: pluto2 builds it into pluto3, which must equal pluto2 byte for
//     byte (both are built from the same source by compilers that behave
//     identically); with -corpus, pluto2 is checked against stage 0 too.
//
// Each stage's build is timed, and its resident memory is sampled every
// second; a build past -limit-mb is killed and reported with the numbers.
// Every stage builds with the same flags (-flags, default
// "-O2 --gencheck=false"). The unchecked mode makes the compilers of stages
// 2 and 3 about nine times faster than a checked build ("-O2"): a checked build looks up every heap access
// in the generation directory, whose cost grows with the heap, and the
// compiler's arena is never freed. -pluto1 starts from a stage 1 compiler
// built before.
//
// Usage:
//
//	go run ./tools/bootstrap [-pluto0 file] [-stages 3] [-corpus] [-work dir] [-limit-mb 24000] [-flags "-O2 --gencheck=false"] [-pluto1 file] [-lib-every 3] [-j 8]
//
// Run it from the repository root. PLUTOROOT is set to ./lib for every
// stage, so that all of them compile the same standard library.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	stages := flag.Int("stages", 3, "the last stage to run (1, 2 or 3)")
	corpus := flag.Bool("corpus", false, "check pluto1 (and pluto2) against the Go-hosted compiler over the test corpus")
	work := flag.String("work", "", "directory for the compilers and the corpus builds (default: a temporary directory)")
	limit := flag.Int("limit-mb", 24000, "kill a stage's build past this resident size, in MB")
	flags := flag.String("flags", "-O2 --gencheck=false", "flags of every stage's build of compiler/cmd/pluto")
	libEvery := flag.Int("lib-every", 3, "check one in every n programs of testdata/lib")
	jobs := flag.Int("j", 8, "corpus builds to run at once")
	zero := flag.String("pluto0", "", "start from this compiler instead of the release one (\"go-cutover\": build that tag's Go-hosted compiler)")
	from := flag.String("pluto1", "", "start from this stage 1 compiler instead of building one (stage 2 onwards)")
	only := flag.String("only", "", "check only the corpus programs whose names contain one of these comma-separated strings")
	flag.Parse()
	if err := run(&options{stages: *stages, corpus: *corpus, work: *work, limitMB: *limit,
		flags: strings.Fields(*flags), libEvery: *libEvery, jobs: *jobs, only: *only, pluto0: *zero, pluto1: *from}); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap:", err)
		os.Exit(1)
	}
}

type options struct {
	stages   int
	corpus   bool
	work     string
	limitMB  int
	flags    []string
	libEvery int
	jobs     int
	only     string
	pluto0   string // a stage 0 compiler to start from, or ""
	pluto1   string // a stage 1 compiler to start from, or ""
}

func run(o *options) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "compiler", "cmd", "pluto")); err != nil {
		return fmt.Errorf("run from the repository root: %w", err)
	}
	if o.work == "" {
		if o.work, err = os.MkdirTemp("", "pluto-bootstrap-"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(o.work, 0o755); err != nil {
		return err
	}
	lib := filepath.Join(root, "lib")
	pluto0 := o.pluto0
	switch pluto0 {
	case "":
		pluto0 = filepath.Join(o.work, "pluto0")
		if err := downloadStage0(pluto0); err != nil {
			return fmt.Errorf("stage 0: %w", err)
		}
	case stage0Tag:
		pluto0 = filepath.Join(o.work, "pluto0")
		if err := buildStage0(root, o.work, pluto0); err != nil {
			return fmt.Errorf("stage 0: %w", err)
		}
	default:
		fmt.Printf("stage 0: %s\n", pluto0)
	}
	prev := pluto0
	var built []string
	for stage := 1; stage <= o.stages; stage++ {
		out := filepath.Join(o.work, fmt.Sprintf("pluto%d", stage))
		if stage == 1 && o.pluto1 != "" {
			fmt.Printf("stage 1: %s\n", o.pluto1)
			built = append(built, o.pluto1)
			prev = o.pluto1
			continue
		}
		m, err := buildStage(root, lib, prev, out, o.flags, o.limitMB)
		fmt.Printf("stage %d: %s\n", stage, m)
		if err != nil {
			return fmt.Errorf("stage %d: %w", stage, err)
		}
		built = append(built, out)
		if o.corpus && stage <= 2 {
			if err := checkCorpus(root, lib, o, pluto0, out); err != nil {
				return fmt.Errorf("stage %d corpus: %w", stage, err)
			}
		}
		prev = out
	}
	if o.stages >= 3 {
		a, err := os.ReadFile(built[1])
		if err != nil {
			return err
		}
		b, err := os.ReadFile(built[2])
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			return fmt.Errorf("pluto2 and pluto3 differ (%d and %d bytes)", len(a), len(b))
		}
		fmt.Printf("pluto2 and pluto3 are identical (%d bytes)\n", len(a))
	}
	return nil
}

// The release compiler that stage 0 starts from by default.
const (
	releaseRepo   = "matjam/plutonium"
	releaseTag    = "bootstrap-20261027"
	releaseAsset  = "pluto-x86_64-linux"
	releaseURL    = "https://github.com/" + releaseRepo + "/releases/download/" + releaseTag + "/" + releaseAsset
	releaseSHA256 = "cbb4171302b0e0a0d446a01f10fb93653ae4c28f6b6b759529f46f7526895c18"
)

// downloadStage0 downloads the release compiler into out and checks its
// SHA-256. It uses gh when it is installed, which also works while the
// repository is private, and plain HTTP otherwise.
func downloadStage0(out string) error {
	data, err := fetchRelease()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != releaseSHA256 {
		return fmt.Errorf("%s: SHA-256 %s, want %s", releaseURL, got, releaseSHA256)
	}
	return os.WriteFile(out, data, 0o755)
}

func fetchRelease() ([]byte, error) {
	if _, err := exec.LookPath("gh"); err == nil {
		fmt.Printf("stage 0: gh release download %s\n", releaseTag)
		cmd := exec.Command("gh", "release", "download", releaseTag, "--repo", releaseRepo,
			"--pattern", releaseAsset, "--output", "-")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err == nil {
			return data, nil
		}
		fmt.Printf("stage 0: gh failed (%v: %s), trying %s\n", err, strings.TrimSpace(stderr.String()), releaseURL)
	} else {
		fmt.Printf("stage 0: %s\n", releaseURL)
	}
	resp, err := http.Get(releaseURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", releaseURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// stage0Tag is the last commit with the Go-hosted compiler.
const stage0Tag = "go-cutover"

// buildStage0 builds the Go-hosted compiler of stage0Tag into out: it checks
// the tag out into a worktree under work, runs go build there and removes
// the worktree.
func buildStage0(root, work, out string) error {
	fmt.Printf("stage 0: go build ./cmd/pluto at %s\n", stage0Tag)
	tree := filepath.Join(work, stage0Tag)
	git := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, b)
		}
		return nil
	}
	if err := git("worktree", "add", "--detach", tree, stage0Tag); err != nil {
		return err
	}
	defer git("worktree", "remove", "--force", tree)
	cmd := exec.Command("go", "build", "-o", out, "./cmd/pluto")
	cmd.Dir = tree
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %v\n%s", err, b)
	}
	return nil
}

// A measure is the cost of one build.
type measure struct {
	wall   time.Duration
	cpu    time.Duration
	peakMB int
	killed bool
}

func (m measure) String() string {
	s := fmt.Sprintf("%.1f s wall, %.1f s CPU, peak resident %d MB", m.wall.Seconds(), m.cpu.Seconds(), m.peakMB)
	if m.killed {
		s += " (killed at the limit)"
	}
	return s
}

// buildStage builds compiler/cmd/pluto with compiler into out, sampling its
// resident size every second and killing it past limitMB.
func buildStage(root, lib, compiler, out string, flags []string, limitMB int) (measure, error) {
	args := append(append([]string{"build"}, flags...), "-o", out, "./compiler/cmd/pluto")
	cmd := exec.Command(compiler, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PLUTOROOT="+lib)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = os.Stdout, &stderr
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return measure{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var m measure
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var err error
wait:
	for {
		select {
		case err = <-done:
			break wait
		case <-tick.C:
			rss := residentMB(cmd.Process.Pid)
			m.peakMB = max(m.peakMB, rss)
			if limitMB > 0 && rss > limitMB {
				m.killed = true
				_ = cmd.Process.Kill()
			}
		}
	}
	m.wall = time.Since(start)
	if st := cmd.ProcessState; st != nil {
		m.cpu = st.UserTime() + st.SystemTime()
		if ru, ok := maxRSS(st); ok {
			m.peakMB = max(m.peakMB, ru)
		}
	}
	if err != nil {
		return m, fmt.Errorf("%s %s: %v\n%s", filepath.Base(compiler), strings.Join(args, " "), err, stderr.String())
	}
	return m, nil
}

// residentMB returns the resident size of process pid in MB, or 0.
func residentMB(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			var kb int
			if _, err := fmt.Sscan(rest, &kb); err == nil {
				return kb / 1024
			}
		}
	}
	return 0
}
