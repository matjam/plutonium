package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A program is one program of the corpus and how it is built.
type program struct {
	name  string
	dir   string   // a directory with the program, or
	file  string   // a single .p file, built as main.p
	flags []string // extra build flags
	diag  bool     // a diagnostics program: the build fails, and the output is the diagnostics
}

// corpus lists the programs the stages are checked on: every example, every
// program of testdata/run and trace, one in every libEvery of testdata/lib,
// the kernel and C ABI programs, and the diagnostics programs of
// cmd/pluto/testdata/diag (those that pluto build compiles).
func corpus(root string, libEvery int) ([]program, error) {
	var out []program
	examples, _ := filepath.Glob(filepath.Join(root, "examples", "*", "main.p"))
	for _, f := range examples {
		out = append(out, program{name: "examples/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, k := range []struct {
		name  string
		flags []string
	}{
		{"sched", []string{"--mode", "kernel"}}, {"smoke", []string{"--mode", "kernel"}},
		{"gsuser", []string{"--mode", "kernel"}}, {"irq", []string{"--mode", "freestanding", "--ldscript", "kernel.ld"}},
	} {
		out = append(out, program{name: "kernel/" + k.name, dir: filepath.Join(root, "testdata", "kernel", k.name), flags: k.flags})
	}
	out = append(out, program{name: "cabi", dir: filepath.Join(root, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "trace"} {
		files, _ := filepath.Glob(filepath.Join(root, "testdata", d, "*.p"))
		for i, f := range files {
			if d == "lib" && i%libEvery != 0 {
				continue
			}
			out = append(out, program{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	diags, _ := filepath.Glob(filepath.Join(root, "cmd", "pluto", "testdata", "diag", "*"))
	for _, d := range diags {
		if _, err := os.Stat(filepath.Join(d, "command")); err == nil {
			continue // another subcommand, such as pluto test
		}
		p := program{name: "diag/" + filepath.Base(d), dir: d, diag: true}
		if flags, err := os.ReadFile(filepath.Join(d, "flags")); err == nil {
			p.flags = strings.Fields(string(flags))
		}
		out = append(out, p)
	}
	if len(out) < 200 {
		return nil, fmt.Errorf("only %d programs in the corpus", len(out))
	}
	return out, nil
}

// knownDifferences are the programs whose diagnostics the translated
// compiler prints differently, and why. Their exit status must still agree.
var knownDifferences = map[string]string{
	// The type checker shows the fix for an owner type declaration as
	// printed source (types2.DeclPrinter); the printer is not translated
	// (it needs text/tabwriter and go/doc/comment), so the translation
	// shows the declaration's description instead (selfhost/internal/load,
	// printhook.p).
	"diag/owners": "no source printer in the translation",
}

// A result is what one compiler made of one build.
type result struct {
	status int
	stderr []byte
	output []byte // the executable or object, if the build succeeded
	took   time.Duration
}

// checkCorpus builds every program of the corpus with want (the Go-hosted
// compiler) and got, at -O0 and -O2 (diagnostics programs also with
// --brief), and requires the same exit status, the same standard error and
// the same executable.
func checkCorpus(root, lib string, o *options, want, got string) error {
	progs, err := corpus(root, o.libEvery)
	if err != nil {
		return err
	}
	var todo []program
	for _, p := range progs {
		for _, only := range strings.Split(o.only, ",") {
			if strings.Contains(p.name, only) {
				todo = append(todo, p)
				break
			}
		}
	}
	work, err := os.MkdirTemp(o.work, "corpus-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	fmt.Printf("checking %s against %s on %d programs\n", filepath.Base(got), filepath.Base(want), len(todo))
	var (
		mu       sync.Mutex
		failures []string
		total    time.Duration
		slowest  time.Duration
		slowName string
		builds   int
	)
	jobs := make(chan program)
	var wg sync.WaitGroup
	for range max(o.jobs, 1) {
		wg.Go(func() {
			for p := range jobs {
				msgs, took := checkProgram(lib, work, p, want, got)
				mu.Lock()
				failures = append(failures, msgs...)
				for name, d := range took {
					total += d
					builds++
					if d > slowest {
						slowest, slowName = d, name
					}
				}
				status := "ok"
				if len(msgs) > 0 {
					status = "DIFFERS"
				}
				fmt.Printf("  %-40s %s\n", p.name, status)
				mu.Unlock()
			}
		})
	}
	for _, p := range todo {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	if builds > 0 {
		fmt.Printf("%s: %d builds, %.0f s in all, %.1f s on average, the slowest %.1f s (%s)\n",
			filepath.Base(got), builds, total.Seconds(), total.Seconds()/float64(builds), slowest.Seconds(), slowName)
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("%d differences:\n%s", len(failures), strings.Join(failures, "\n"))
	}
	fmt.Printf("%s matches %s on every program\n", filepath.Base(got), filepath.Base(want))
	return nil
}

// checkProgram builds p with both compilers and returns the differences,
// and how long each build of got took.
func checkProgram(lib, work string, p program, want, got string) ([]string, map[string]time.Duration) {
	took := map[string]time.Duration{}
	dir := p.dir
	base := filepath.Join(work, strings.ReplaceAll(p.name, "/", "_"))
	if p.file != "" {
		dir = base + ".src"
		src, err := os.ReadFile(p.file)
		if err == nil {
			err = os.MkdirAll(dir, 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644)
		}
		if err != nil {
			return []string{fmt.Sprintf("%s: %v", p.name, err)}, took
		}
	}
	type variant struct {
		name  string
		flags []string
	}
	variants := []variant{{"O0", []string{"-O0"}}, {"O2", []string{"-O2"}}}
	if p.diag {
		variants = append(variants, variant{"brief", []string{"-O2", "--brief"}})
	}
	var msgs []string
	for _, v := range variants {
		out := base + "." + v.name + ".out"
		args := append(append(append([]string{"build"}, v.flags...), p.flags...), "-o", out, ".")
		w := buildOnce(lib, dir, out, want, args)
		g := buildOnce(lib, dir, out, got, args)
		took[p.name+" "+v.name] = g.took
		name := p.name + " " + v.name
		switch {
		case w.status != g.status:
			msgs = append(msgs, fmt.Sprintf("%s: exit status %d, want %d\n%s", name, g.status, w.status, firstDifference(w.stderr, g.stderr)))
		case knownDifferences[p.name] != "":
			// Only the exit status is compared.
		case !bytes.Equal(w.stderr, g.stderr):
			msgs = append(msgs, fmt.Sprintf("%s: standard error differs\n%s", name, firstDifference(w.stderr, g.stderr)))
		case !bytes.Equal(w.output, g.output):
			at := 0
			for at < len(w.output) && at < len(g.output) && w.output[at] == g.output[at] {
				at++
			}
			msgs = append(msgs, fmt.Sprintf("%s: the executables differ: %d bytes, want %d; first difference at byte %d", name, len(g.output), len(w.output), at))
		case p.diag && w.status == 0:
			msgs = append(msgs, fmt.Sprintf("%s: the diagnostics program built", name))
		}
	}
	return msgs, took
}

// buildOnce runs compiler with args in dir and collects the result; out is
// the output file the arguments name, which is read and removed.
func buildOnce(lib, dir, out, compiler string, args []string) result {
	_ = os.Remove(out)
	cmd := exec.Command(compiler, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLUTOROOT="+lib)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	start := time.Now()
	err := cmd.Run()
	// The work directory's name differs on every build; the linker's
	// messages name its files.
	r := result{stderr: workDir.ReplaceAll(stderr.Bytes(), []byte("pluto-build-N")), took: time.Since(start)}
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		r.status = ee.ExitCode()
	case err != nil:
		r.status = -1
		r.stderr = append(r.stderr, err.Error()...)
	}
	if r.status == 0 {
		r.output, _ = os.ReadFile(out)
	}
	_ = os.Remove(out)
	return r
}

// workDir matches the name of a build's temporary directory.
var workDir = regexp.MustCompile(`pluto-build-[0-9]+`)

// firstDifference shows the first line where two texts differ.
func firstDifference(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("    line %d:\n    want: %q\n    got:  %q", i+1, wl, gl)
		}
	}
	return "    (same text)"
}
