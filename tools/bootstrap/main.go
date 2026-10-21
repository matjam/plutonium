// Command bootstrap runs the stages of the self-hosting plan (section 6.1)
// and checks them:
//
//   - stage 0: go build of cmd/pluto, the Go-hosted compiler;
//   - stage 1: stage 0 builds selfhost/cmd/pluto, the driver and compiler
//     translated to Plutonium, into pluto1;
//   - with -corpus, pluto1 and stage 0 build the programs of the test corpus
//     (corpus.go) at -O0 and -O2, and every executable (or object file),
//     diagnostic and exit status must be byte-identical;
//   - stage 2: pluto1 builds selfhost/cmd/pluto into pluto2;
//   - stage 3: pluto2 builds it into pluto3, which must equal pluto2 byte for
//     byte (both are built from the same source by compilers that behave
//     identically); with -corpus, pluto2 is checked against stage 0 too.
//
// Each stage's build is timed, and its resident memory is sampled every
// second; a build past -limit-mb is killed and reported with the numbers.
// Every stage builds with the same flags (-flags, default "-O2"). The
// unchecked mode ("-O2 --gencheck=false") makes the compilers of stages 2
// and 3 about nine times faster: a checked build looks up every heap access
// in the generation directory, whose cost grows with the heap, and the
// compiler's arena is never freed. -pluto1 starts from a stage 1 compiler
// built before.
//
// Usage:
//
//	go run ./tools/bootstrap [-stages 3] [-corpus] [-work dir] [-limit-mb 24000] [-flags "-O2"] [-pluto1 file] [-lib-every 3] [-j 8]
//
// Run it from the repository root. PLUTOROOT is set to ./lib for every
// stage, so that all of them compile the same standard library.
package main

import (
	"bytes"
	"flag"
	"fmt"
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
	flags := flag.String("flags", "-O2", "flags of every stage's build of selfhost/cmd/pluto")
	libEvery := flag.Int("lib-every", 3, "check one in every n programs of testdata/lib")
	jobs := flag.Int("j", 8, "corpus builds to run at once")
	from := flag.String("pluto1", "", "start from this stage 1 compiler instead of building one (stage 2 onwards)")
	only := flag.String("only", "", "check only the corpus programs whose names contain one of these comma-separated strings")
	flag.Parse()
	if err := run(&options{stages: *stages, corpus: *corpus, work: *work, limitMB: *limit,
		flags: strings.Fields(*flags), libEvery: *libEvery, jobs: *jobs, only: *only, pluto1: *from}); err != nil {
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
	pluto1   string // a stage 1 compiler to start from, or ""
}

func run(o *options) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "selfhost", "cmd", "pluto")); err != nil {
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
	pluto0 := filepath.Join(o.work, "pluto0")
	fmt.Printf("stage 0: go build ./cmd/pluto\n")
	if out, err := exec.Command("go", "build", "-o", pluto0, "./cmd/pluto").CombinedOutput(); err != nil {
		return fmt.Errorf("stage 0: %v\n%s", err, out)
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

// buildStage builds selfhost/cmd/pluto with compiler into out, sampling its
// resident size every second and killing it past limitMB.
func buildStage(root, lib, compiler, out string, flags []string, limitMB int) (measure, error) {
	args := append(append([]string{"build"}, flags...), "-o", out, "./selfhost/cmd/pluto")
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
