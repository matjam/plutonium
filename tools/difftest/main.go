// Command difftest is a differential fuzzer for the Plutonium compiler.
//
// It generates random programs in the common subset of Go and Plutonium,
// runs each with Go and with Plutonium at -O0 and -O2, and reports any
// difference in output or exit status, Plutonium compile errors and
// compiler crashes. With -reduce it shrinks each failing program to a
// small reproducer. Run it from the repository root:
//
//	go run ./tools/difftest -seed 1 -n 1000 -j 8 -o /tmp/findings -reduce
//	go run ./tools/difftest -seed 42 -print   # show one program
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

func main() {
	seed := flag.Int64("seed", 1, "seed of the first program")
	n := flag.Int("n", 100, "number of programs")
	outDir := flag.String("o", "", "directory to keep failing programs in (default: do not keep them)")
	jobs := flag.Int("j", max(runtime.NumCPU()/2, 1), "programs to test in parallel")
	doReduce := flag.Bool("reduce", false, "reduce failing programs to small reproducers")
	pu := flag.String("pu", "", "Plutonium compiler binary (default: build ./cmd/pluto)")
	work := flag.String("work", "", "scratch directory (default: a new temporary directory)")
	show := flag.Bool("print", false, "print the program for -seed and exit")
	rangeWrites := flag.Bool("rangewrites", false, "let loops write to the array they range over")
	flag.Parse()

	opt := Options{RangeWrites: *rangeWrites}
	if *show {
		fmt.Print(Generate(*seed, opt))
		return
	}

	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	cleanup := func() {}
	if *work == "" {
		*work, err = os.MkdirTemp("", "difftest")
		if err != nil {
			log.Fatal(err)
		}
		dir := *work
		cleanup = func() { _ = os.RemoveAll(dir) }
	} else if err := os.MkdirAll(*work, 0o755); err != nil {
		log.Fatal(err)
	}
	if *pu == "" {
		*pu = filepath.Join(*work, "pu")
		cmd := exec.Command("go", "build", "-o", *pu, "./cmd/pluto")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("building the compiler: %v", err)
		}
	}
	r := &runner{pu: *pu, plutoDir: filepath.Join(root, "lib"), work: *work, runLimit: runTimeout}

	var (
		mu      sync.Mutex
		bySig   = map[string][]int64{}
		genBugs []int64
		done    int
		start   = time.Now()
	)
	seeds := make(chan int64)
	var wg sync.WaitGroup
	for w := 0; w < *jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range seeds {
				src := Generate(s, opt)
				v := r.check(src, "")
				mu.Lock()
				done++
				switch {
				case v.genBug != "":
					genBugs = append(genBugs, s)
					log.Printf("seed %d: generator bug: %s", s, tail(v.genBug, 5))
				case v.failed():
					bySig[v.signature()] = append(bySig[v.signature()], s)
					log.Printf("seed %d: FAIL %s", s, v.signature())
				}
				if done%100 == 0 {
					log.Printf("%d programs, %d failing, %s", done, countFailing(bySig), time.Since(start).Round(time.Second))
				}
				mu.Unlock()
				if v.genBug == "" && !v.failed() {
					continue
				}
				if *outDir != "" {
					save(*outDir, s, src, v, *doReduce, r)
				}
			}
		}()
	}
	for s := *seed; s < *seed+int64(*n); s++ {
		seeds <- s
	}
	close(seeds)
	wg.Wait()

	fmt.Printf("\n%d programs (seeds %d..%d) in %s\n", *n, *seed, *seed+int64(*n)-1, time.Since(start).Round(time.Second))
	if len(genBugs) > 0 {
		fmt.Printf("generator bugs: %v\n", genBugs)
	}
	var sigs []string
	for s := range bySig {
		sigs = append(sigs, s)
	}
	sort.Strings(sigs)
	for _, s := range sigs {
		fmt.Printf("%4d  %s  seeds %v\n", len(bySig[s]), s, firstN(bySig[s], 8))
	}
	cleanup()
	if len(sigs) > 0 || len(genBugs) > 0 {
		os.Exit(1)
	}
}

func countFailing(m map[string][]int64) int {
	n := 0
	for _, s := range m {
		n += len(s)
	}
	return n
}

func firstN(s []int64, n int) []int64 {
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[:min(n, len(s))]
}

// save writes a failing program and its report, and with reduce also a
// reduced reproducer.
func save(dir string, seed int64, src string, v *verdict, reduce bool, r *runner) {
	d := filepath.Join(dir, fmt.Sprintf("seed-%d", seed))
	if err := os.MkdirAll(d, 0o755); err != nil {
		log.Print(err)
		return
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(d, name), []byte(text), 0o644); err != nil {
			log.Print(err)
		}
	}
	write("orig.go", src)
	if v.genBug != "" {
		write("report.txt", "generator bug\n"+v.genBug+"\n")
		return
	}
	write("report.txt", v.report())
	if !reduce {
		return
	}
	rr := *r
	rr.runLimit = 2 * time.Second
	rd := &reducer{r: &rr, sig: v.signature(), logf: func(string, ...any) {}}
	small := rd.reduce(src)
	write("reduced.go", small)
	final := rr.check(small, "")
	write("reduced-report.txt", final.report())
	log.Printf("seed %d: reduced %d -> %d bytes in %d tests: %s", seed, len(src), len(small), rd.tests, d)
}
