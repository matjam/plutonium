// Command run builds every benchmark in bench/ for C, Rust, Go and
// Plutonium, verifies each binary's output against expected.txt, times it,
// and prints a comparison table.
//
// Run from the repository root:
//
//	go run ./bench/run.go [-n 5] [-only fib,sieve] [-langs c,rust,go,pu] [-pluto path] [-json out.json]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
)

// benchmarks lists every benchmark directory, in table order.
var benchmarks = []string{
	"fib", "sieve", "nbody", "spectralnorm", "mandelbrot", "fannkuch",
	"binarytrees", "matmul", "collatz", "heapsort", "quicksort", "crc32",
	"sha256", "fasta-lcg", "structs", "strings",
}

// lang describes one implementation language.
type lang struct {
	key   string // flag value and JSON key
	title string // table column heading
}

var allLangs = []lang{
	{"c", "C"},
	{"rust", "Rust"},
	{"go", "Go"},
	{"pu", "Plutonium"},
}

// Result is the outcome of building, verifying and timing one benchmark in
// one language.
type Result struct {
	OK       bool    `json:"ok"`
	Stage    string  `json:"stage,omitempty"` // build, run, verify, timing
	Error    string  `json:"error,omitempty"`
	MedianNS int64   `json:"median_ns,omitempty"`
	RunsNS   []int64 `json:"runs_ns,omitempty"`
	BuildDur float64 `json:"build_seconds,omitempty"`
}

// Report is the JSON document written by -json.
type Report struct {
	Date        string                       `json:"date"`
	GOOS        string                       `json:"goos"`
	GOARCH      string                       `json:"goarch"`
	Runs        int                          `json:"runs"`
	Langs       []string                     `json:"langs"`
	Toolchains  map[string]string            `json:"toolchains"`
	Results     map[string]map[string]Result `json:"results"`
	GeomeanVsRS map[string]*float64          `json:"geomean_vs_rust,omitempty"`
}

type options struct {
	runs       int
	only       []string
	langs      []lang
	pluto      string
	jsonOut    string
	cc         string
	timeout    time.Duration
	gomaxprocs int
	keep       bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		runs       = flag.Int("n", 5, "timed runs per binary (the median is reported)")
		only       = flag.String("only", "", "comma-separated benchmark names to run (default all)")
		langsFlag  = flag.String("langs", "c,rust,go,pu", "comma-separated languages: c, rust, go, pu")
		pluto      = flag.String("pluto", "", "path to the pluto compiler (default: pluto on PATH)")
		jsonOut    = flag.String("json", "", "write results as JSON to this file")
		cc         = flag.String("cc", "gcc", "C compiler")
		timeout    = flag.Duration("timeout", 60*time.Second, "per-run time limit")
		gomaxprocs = flag.Int("gomaxprocs", 1, "GOMAXPROCS for Go binaries (0 leaves the Go default)")
		keep       = flag.Bool("keep", false, "keep the temporary build directory")
	)
	flag.Parse()
	if *runs < 1 {
		return errors.New("-n must be at least 1")
	}

	opts := options{
		runs:       *runs,
		pluto:      *pluto,
		jsonOut:    *jsonOut,
		cc:         *cc,
		timeout:    *timeout,
		gomaxprocs: *gomaxprocs,
		keep:       *keep,
	}
	for _, key := range splitList(*langsFlag) {
		i := slices.IndexFunc(allLangs, func(l lang) bool { return l.key == key })
		if i < 0 {
			return fmt.Errorf("unknown language %q (want c, rust, go, pu)", key)
		}
		if !slices.ContainsFunc(opts.langs, func(l lang) bool { return l.key == key }) {
			opts.langs = append(opts.langs, allLangs[i])
		}
	}
	if len(opts.langs) == 0 {
		return errors.New("-langs selects no languages")
	}
	// Keep columns in canonical order regardless of flag order.
	sort.SliceStable(opts.langs, func(i, j int) bool {
		return langIndex(opts.langs[i].key) < langIndex(opts.langs[j].key)
	})
	for _, name := range splitList(*only) {
		if !slices.Contains(benchmarks, name) {
			return fmt.Errorf("unknown benchmark %q (have %s)", name, strings.Join(benchmarks, ", "))
		}
		opts.only = append(opts.only, name)
	}

	benchDir, err := findBenchDir()
	if err != nil {
		return err
	}
	repoRoot := filepath.Dir(benchDir)

	work, err := os.MkdirTemp("", "plutonium-bench-")
	if err != nil {
		return err
	}
	if opts.keep {
		fmt.Fprintf(os.Stderr, "build directory: %s\n", work)
	} else {
		defer os.RemoveAll(work)
	}

	selected := benchmarks
	if len(opts.only) > 0 {
		selected = opts.only
	}

	// Resolve the Plutonium compiler once.
	var plutoBin string
	var plutoErr error
	if hasLang(opts.langs, "pu") {
		plutoBin, plutoErr = resolvePluto(opts.pluto)
		if plutoErr != nil {
			fmt.Fprintf(os.Stderr, "pluto compiler unavailable: %v\n", plutoErr)
		}
	}

	report := Report{
		Date:       time.Now().UTC().Format(time.RFC3339),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		Runs:       opts.runs,
		Toolchains: toolchainVersions(opts, plutoBin),
		Results:    map[string]map[string]Result{},
	}
	for _, l := range opts.langs {
		report.Langs = append(report.Langs, l.key)
	}

	for _, name := range selected {
		srcDir := filepath.Join(benchDir, name)
		expected, err := os.ReadFile(filepath.Join(srcDir, "expected.txt"))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		report.Results[name] = map[string]Result{}
		for _, l := range opts.langs {
			fmt.Fprintf(os.Stderr, "%-13s %-9s ", name, l.title)
			var res Result
			if l.key == "pu" && plutoErr != nil {
				res = Result{Stage: "build", Error: "compiler unavailable: " + firstLine(plutoErr.Error())}
			} else {
				res = benchOne(opts, l.key, name, srcDir, filepath.Join(work, name, l.key), repoRoot, plutoBin, expected)
			}
			if res.OK {
				fmt.Fprintf(os.Stderr, "%8.3fs\n", seconds(res.MedianNS))
			} else {
				fmt.Fprintf(os.Stderr, "FAIL (%s): %s\n", res.Stage, firstLine(summarize(res.Error)))
			}
			report.Results[name][l.key] = res
		}
	}

	report.GeomeanVsRS = printTable(os.Stdout, opts, selected, report.Results)

	if opts.jsonOut != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(opts.jsonOut, append(data, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", opts.jsonOut)
	}
	return nil
}

// benchOne builds, verifies and times a single benchmark/language pair.
func benchOne(opts options, langKey, name, srcDir, outDir, repoRoot, plutoBin string, expected []byte) Result {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{Stage: "build", Error: err.Error()}
	}
	bin := filepath.Join(outDir, name)
	start := time.Now()
	if err := build(opts, langKey, srcDir, outDir, bin, repoRoot, plutoBin); err != nil {
		return Result{Stage: "build", Error: err.Error()}
	}
	res := Result{BuildDur: time.Since(start).Seconds()}

	env := os.Environ()
	if langKey == "go" && opts.gomaxprocs > 0 {
		env = append(env, fmt.Sprintf("GOMAXPROCS=%d", opts.gomaxprocs))
	}

	// The first run verifies the output and doubles as a warm-up.
	out, _, err := execute(bin, env, opts.timeout)
	if err != nil {
		res.Stage, res.Error = "run", err.Error()
		return res
	}
	if !bytes.Equal(out, expected) {
		res.Stage, res.Error = "verify", describeMismatch(out, expected)
		return res
	}

	for i := 0; i < opts.runs; i++ {
		out, d, err := execute(bin, env, opts.timeout)
		if err != nil {
			res.Stage, res.Error = "timing", err.Error()
			return res
		}
		if !bytes.Equal(out, expected) {
			res.Stage, res.Error = "timing", "output changed between runs: "+describeMismatch(out, expected)
			return res
		}
		res.RunsNS = append(res.RunsNS, d.Nanoseconds())
	}
	res.MedianNS = median(res.RunsNS)
	res.OK = true
	return res
}

// build compiles one benchmark into bin.
func build(opts options, langKey, srcDir, outDir, bin, repoRoot, plutoBin string) error {
	switch langKey {
	case "c":
		return command(srcDir, nil, opts.cc,
			"-std=c11", "-O2", "-ffp-contract=off", "-march=x86-64",
			"-o", bin, filepath.Join(srcDir, "main.c"), "-lm")
	case "rust":
		return command(srcDir, nil, "rustc",
			"-C", "opt-level=3", "--edition", "2021",
			"-o", bin, filepath.Join(srcDir, "main.rs"))
	case "go":
		// Build from a private module so the .c/.rs files next to main.go
		// and the bench module itself play no part.
		modDir := filepath.Join(outDir, "gomod")
		if err := os.MkdirAll(modDir, 0o755); err != nil {
			return err
		}
		src, err := os.ReadFile(filepath.Join(srcDir, "main.go"))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(modDir, "main.go"), src, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte("module benchmark\n\ngo 1.26\n"), 0o644); err != nil {
			return err
		}
		env := []string{"GOAMD64=v1", "GOWORK=off", "GOFLAGS="}
		return command(modDir, env, "go", "build", "-o", bin, ".")
	case "pu":
		env := []string{}
		if os.Getenv("PLUTOROOT") == "" {
			env = append(env, "PLUTOROOT="+filepath.Join(repoRoot, "lib"))
		}
		return command(repoRoot, env, plutoBin, "build", "-o", bin, filepath.Join(srcDir, "pu"))
	}
	return fmt.Errorf("unknown language %q", langKey)
}

// command runs a build step, returning its combined output on failure.
func command(dir string, extraEnv []string, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// execute runs a benchmark binary and returns its stdout and wall time.
func execute(bin string, env []string, timeout time.Duration) ([]byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	d := time.Since(start)
	if ctx.Err() == context.DeadlineExceeded {
		return nil, d, fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		msg := err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg = exitErr.ProcessState.String() // e.g. "exit status 3" or "signal: segmentation fault"
		}
		if s := strings.TrimSpace(stderr.String()); s != "" {
			msg += ": " + firstLine(s)
		}
		return nil, d, errors.New(msg)
	}
	return stdout.Bytes(), d, nil
}

// resolvePluto returns the compiler path: the -pluto flag if given,
// otherwise pluto on PATH.
func resolvePluto(flagPath string) (string, error) {
	if flagPath != "" {
		p, err := filepath.Abs(flagPath)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(p); err != nil {
			return "", err
		}
		return p, nil
	}
	bin, err := exec.LookPath("pluto")
	if err != nil {
		return "", errors.New("no pluto on PATH: install it or pass -pluto /path/to/pluto")
	}
	return bin, nil
}

// printTable writes the results table and returns the geometric means of
// each language's time relative to Rust.
func printTable(w *os.File, opts options, names []string, results map[string]map[string]Result) map[string]*float64 {
	header := []string{"benchmark"}
	for _, l := range opts.langs {
		header = append(header, l.title)
	}
	hasRust := hasLang(opts.langs, "rust")
	var ratioLangs []lang
	if hasRust {
		for _, key := range []string{"pu", "go", "c"} {
			if hasLang(opts.langs, key) {
				ratioLangs = append(ratioLangs, allLangs[langIndex(key)])
			}
		}
	}
	for _, l := range ratioLangs {
		header = append(header, ratioLabel(l.key))
	}

	logSums := map[string]float64{}
	counts := map[string]int{}
	var rows [][]string
	var failures []string
	for _, name := range names {
		row := []string{name}
		for _, l := range opts.langs {
			r := results[name][l.key]
			if r.OK {
				row = append(row, fmt.Sprintf("%.3f", seconds(r.MedianNS)))
			} else {
				row = append(row, "FAIL ("+r.Stage+")")
				failures = append(failures, fmt.Sprintf("%s/%s: %s failed: %s", name, l.key, r.Stage, summarize(r.Error)))
			}
		}
		rust := results[name]["rust"]
		for _, l := range ratioLangs {
			r := results[name][l.key]
			if r.OK && rust.OK && rust.MedianNS > 0 {
				ratio := float64(r.MedianNS) / float64(rust.MedianNS)
				logSums[l.key] += math.Log(ratio)
				counts[l.key]++
				row = append(row, fmt.Sprintf("%.2f", ratio))
			} else {
				row = append(row, "-")
			}
		}
		rows = append(rows, row)
	}

	geo := map[string]*float64{}
	footer := []string{"geomean"}
	for range opts.langs {
		footer = append(footer, "")
	}
	for _, l := range ratioLangs {
		if counts[l.key] == 0 {
			footer = append(footer, "-")
			geo[l.key] = nil
			continue
		}
		g := math.Exp(logSums[l.key] / float64(counts[l.key]))
		geo[l.key] = &g
		footer = append(footer, fmt.Sprintf("%.2f (%d/%d)", g, counts[l.key], len(names)))
	}

	all := append([][]string{header}, rows...)
	all = append(all, footer)
	widths := make([]int, len(header))
	for _, r := range all {
		for i, c := range r {
			widths[i] = max(widths[i], len(c))
		}
	}
	line := func(r []string) {
		var b strings.Builder
		b.WriteString("|")
		for i, c := range r {
			if i == 0 {
				fmt.Fprintf(&b, " %-*s |", widths[i], c)
			} else {
				fmt.Fprintf(&b, " %*s |", widths[i], c)
			}
		}
		fmt.Fprintln(w, b.String())
	}
	sep := func() {
		var b strings.Builder
		b.WriteString("|")
		for i, wd := range widths {
			if i == 0 {
				b.WriteString(" " + strings.Repeat("-", wd) + " |")
			} else {
				b.WriteString(" " + strings.Repeat("-", wd-1) + ": |")
			}
		}
		fmt.Fprintln(w, b.String())
	}

	fmt.Fprintf(w, "\nMedian wall time in seconds over %d runs; ratios are relative to Rust (lower is faster).\n\n", opts.runs)
	line(header)
	sep()
	for _, r := range rows {
		line(r)
	}
	line(footer)

	if len(failures) > 0 {
		fmt.Fprintln(w, "\nFailures:")
		for _, f := range failures {
			fmt.Fprintln(w, "  "+f)
		}
	}
	if len(geo) == 0 {
		return nil
	}
	return geo
}

func ratioLabel(key string) string {
	switch key {
	case "pu":
		return "Pu/Rust"
	case "go":
		return "Go/Rust"
	case "c":
		return "C/Rust"
	}
	return key + "/Rust"
}

// toolchainVersions records compiler versions for the JSON report.
func toolchainVersions(opts options, plutoBin string) map[string]string {
	v := map[string]string{}
	probe := func(key, name string, args ...string) {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err == nil {
			v[key] = firstLine(strings.TrimSpace(string(out)))
		}
	}
	for _, l := range opts.langs {
		switch l.key {
		case "c":
			probe("c", opts.cc, "--version")
		case "rust":
			probe("rust", "rustc", "--version")
		case "go":
			probe("go", "go", "version")
		case "pu":
			if plutoBin != "" {
				probe("pu", plutoBin, "version")
			}
		}
	}
	return v
}

// findBenchDir locates the bench directory: next to this source file when
// run with `go run`, else ./bench or the current directory.
func findBenchDir() (string, error) {
	var candidates []string
	if _, file, _, ok := runtime.Caller(0); ok && filepath.IsAbs(file) {
		candidates = append(candidates, filepath.Dir(file))
	}
	candidates = append(candidates, "bench", ".")
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "fib", "expected.txt")); err == nil {
			return filepath.Abs(c)
		}
	}
	return "", errors.New("cannot find the bench directory; run from the repository root")
}

func describeMismatch(got, want []byte) string {
	g := strings.Split(string(got), "\n")
	w := strings.Split(string(want), "\n")
	for i := 0; i < max(len(g), len(w)); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("output mismatch at line %d: got %q, want %q", i+1, gl, wl)
		}
	}
	return "output mismatch"
}

func median(xs []int64) int64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func seconds(ns int64) float64 { return float64(ns) / 1e9 }

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hasLang(ls []lang, key string) bool {
	return slices.ContainsFunc(ls, func(l lang) bool { return l.key == key })
}

func langIndex(key string) int {
	return slices.IndexFunc(allLangs, func(l lang) bool { return l.key == key })
}

func firstLine(s string) string {
	return firstLines(s, 1)
}

// summarize shortens a failure message for the table: it keeps up to three
// distinct lines that mention an error (dropping CLI usage text), falling back
// to the first lines of the message.
func summarize(s string) string {
	var picked []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimPrefix(strings.TrimSpace(l), "Error: ")
		if l == "" || !strings.Contains(strings.ToLower(l), "error") || slices.Contains(picked, l) {
			continue
		}
		picked = append(picked, l)
		if len(picked) == 3 {
			break
		}
	}
	if len(picked) == 0 {
		return firstLines(s, 3)
	}
	return strings.Join(picked, " | ")
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, " | ")
}
