package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// levels are the Plutonium optimisation levels each program is run at.
var levels = []int{0, 2}

const (
	buildTimeout = 120 * time.Second
	runTimeout   = 10 * time.Second
)

// A runner builds and runs programs with Go and with Plutonium.
type runner struct {
	pu       string // compiler binary
	plutoDir string // PLUTOROOT
	work     string // scratch directory
	runLimit time.Duration
	puFlags  []string // more flags for Plutonium builds
}

// An outcome is what one toolchain did with a program.
type outcome struct {
	class  string // "ok", "compile-error", "slow-compile", "crash", "signal", "timeout"
	output string // program output, or the compiler's
	status int
}

func (o outcome) String() string {
	if o.class == "ok" {
		return fmt.Sprintf("exit %d", o.status)
	}
	return o.class
}

// A verdict is the result of running one program everywhere.
type verdict struct {
	goOut  outcome
	pu     []outcome // by index into levels
	bad    []bool    // pu[i] differs from Go
	genBug string    // non-empty if the program is not valid Go or misbehaves under Go
	goBug  bool      // Go without optimisation agrees with Plutonium, not with optimised Go
}

func (v *verdict) failed() bool {
	for _, b := range v.bad {
		if b {
			return true
		}
	}
	return false
}

// signature identifies a failure so the reducer keeps the same bug.
func (v *verdict) signature() string {
	var parts []string
	for i, o := range v.pu {
		s := fmt.Sprintf("O%d:", levels[i])
		switch {
		case !v.bad[i]:
			s += "same"
		case o.class == "ok":
			s += "mismatch"
		default:
			s += o.class + ":" + errorKey(o.output)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

var (
	posRE = regexp.MustCompile(`[\w./-]*\.(?:p|go):\d+(?::\d+)?`)
	numRE = regexp.MustCompile(`\b(?:0x[0-9a-f]+|\d+)\b`)
	gorRE = regexp.MustCompile(`goroutine \d+`)
)

// errorKey reduces compiler output to a line that identifies the error
// independently of positions and numbers.
func errorKey(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || gorRE.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, "panic:") || strings.Contains(line, "error") || strings.Contains(line, ": ") {
			line = posRE.ReplaceAllString(line, "POS")
			return numRE.ReplaceAllString(line, "N")
		}
	}
	return ""
}

// check runs src with Go and with Plutonium at every level.
func (r *runner) check(src string, wantSig string) *verdict {
	dir, err := os.MkdirTemp(r.work, "prog")
	if err != nil {
		return &verdict{genBug: err.Error()}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	v := &verdict{}

	v.goOut = r.runGo(dir, src)
	switch {
	case v.goOut.class == "compile-error":
		v.genBug = "Go rejects the program:\n" + v.goOut.output
		return v
	case v.goOut.class != "ok" || v.goOut.status != 0:
		v.genBug = fmt.Sprintf("Go run: %s\n%s", v.goOut, tail(v.goOut.output, 20))
		return v
	}
	for _, lvl := range levels {
		o := r.runPu(dir, src, lvl)
		v.pu = append(v.pu, o)
		v.bad = append(v.bad, o.class != "ok" || o.status != v.goOut.status || o.output != v.goOut.output)
		// Stop early when reducing and the levels so far already differ.
		if wantSig != "" && !strings.HasPrefix(wantSig, v.signature()) {
			return v
		}
	}
	// A program outside the common subset is the generator's (or, while
	// reducing, a mutation's) fault.
	for i, o := range v.pu {
		if l := leftSubset(o.output); v.bad[i] && l != "" {
			v.genBug = "outside the common subset of Go and Plutonium: " + l
			return v
		}
	}
	// When every level agrees with the others but not with Go, Go itself
	// may be wrong: ask it again without optimisation.
	if wantSig == "" && v.agreeAgainstGo() {
		noOpt := r.runGo(dir, src, "-gcflags=all=-N -l")
		if noOpt.class == "ok" && noOpt.status == v.pu[0].status && noOpt.output == v.pu[0].output {
			v.goBug = true
		}
	}
	return v
}

// agreeAgainstGo reports whether every Plutonium level ran and produced
// the same output, which differs from Go's.
func (v *verdict) agreeAgainstGo() bool {
	for i, o := range v.pu {
		if !v.bad[i] || o.class != "ok" || o.output != v.pu[0].output || o.status != v.pu[0].status {
			return false
		}
	}
	return len(v.pu) > 0
}

func (r *runner) runGo(dir, src string, flags ...string) outcome {
	gdir := filepath.Join(dir, "go")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		return outcome{class: "compile-error", output: err.Error()}
	}
	if err := os.WriteFile(filepath.Join(gdir, "main.go"), []byte(src), 0o644); err != nil {
		return outcome{class: "compile-error", output: err.Error()}
	}
	if err := os.WriteFile(filepath.Join(gdir, "go.mod"), []byte("module prog\n\ngo 1.23\n"), 0o644); err != nil {
		return outcome{class: "compile-error", output: err.Error()}
	}
	bin := filepath.Join(dir, "gobin")
	args := append([]string{"build", "-o", bin}, flags...)
	out, err := r.command(buildTimeout, gdir, nil, "go", append(args, ".")...)
	if err != nil {
		return outcome{class: "compile-error", output: out}
	}
	return r.exec(bin)
}

func (r *runner) runPu(dir, src string, level int) outcome {
	pdir := filepath.Join(dir, fmt.Sprintf("pu%d", level))
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		return outcome{class: "compile-error", output: err.Error()}
	}
	if err := os.WriteFile(filepath.Join(pdir, "main.p"), []byte(src), 0o644); err != nil {
		return outcome{class: "compile-error", output: err.Error()}
	}
	bin := filepath.Join(dir, fmt.Sprintf("bin%d", level))
	env := []string{"PLUTOROOT=" + r.plutoDir}
	args := append([]string{"build", "-O", fmt.Sprint(level), "-o", bin}, r.puFlags...)
	out, err := r.command(buildTimeout, pdir, env, r.pu, append(args, ".")...)
	if err != nil {
		var ee *exec.ExitError
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return outcome{class: "slow-compile", output: fmt.Sprintf("compiler took over %s\n", buildTimeout) + out}
		case strings.Contains(out, "goroutine ") && strings.Contains(out, "panic"):
			return outcome{class: "crash", output: out}
		case errors.As(err, &ee) && ee.ExitCode() < 0:
			return outcome{class: "crash", output: "compiler killed: " + err.Error() + "\n" + out}
		}
		return outcome{class: "compile-error", output: out}
	}
	return r.exec(bin)
}

// exec runs a built program and collects its standard output and error.
func (r *runner) exec(bin string) outcome {
	ctx, cancel := context.WithTimeout(context.Background(), r.runLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	o := outcome{class: "ok", output: buf.String()}
	if ctx.Err() != nil {
		o.class = "timeout"
		return o
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		o.status = ee.ExitCode()
		if o.status < 0 {
			o.class = "signal"
			o.output += "\n" + ee.String()
		}
	} else if err != nil {
		o.class = "signal"
		o.output += "\n" + err.Error()
	}
	return o
}

func (r *runner) command(timeout time.Duration, dir string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), context.DeadlineExceeded
	}
	return string(out), err
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{"..."}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}

// report describes a failing verdict.
func (v *verdict) report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "signature: %s\n", v.signature())
	if v.goBug {
		b.WriteString("Go built with -gcflags=all=-N -l agrees with Plutonium: probably a Go compiler bug\n")
	}
	fmt.Fprintf(&b, "go: %s\n", v.goOut)
	for i, o := range v.pu {
		fmt.Fprintf(&b, "pu -O%d: %s", levels[i], o)
		if !v.bad[i] {
			b.WriteString(" (matches Go)\n")
			continue
		}
		b.WriteString("\n")
		if o.class == "ok" {
			b.WriteString(firstDiff(v.goOut.output, o.output))
		} else {
			b.WriteString(tail(o.output, 30) + "\n")
		}
	}
	return b.String()
}

// firstDiff shows where two outputs first differ.
func firstDiff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("  first difference at output line %d:\n    go: %q\n    pu: %q\n", i+1, wl, gl)
		}
	}
	return "  outputs are equal; exit status differs\n"
}
