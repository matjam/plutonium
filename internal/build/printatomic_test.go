package build_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// printAtomicSrc starts tasks that print many lines at once, each line
// built from several print and println pieces of different types. Lines
// are checked afterwards: a line mixed with another statement's output
// does not match its pattern.
const printAtomicSrc = `package main

const pad = "0123456789abcdefghijklmnopqrstuvwxyz"

// long is longer than the runtime's print buffer, so a statement printing
// it is written in several calls.
const long = pad + pad + pad + pad + pad + pad + pad + pad + pad + pad + pad + pad + pad + pad + pad + pad + pad

const (
	tasks = 8
	lines = 400
)

func worker(id int, done chan bool) {
	defer func() { done <- true }()
	defer println("deferred", id, "done")
	for i := 0; i < lines; i++ {
		println("short", id, "line", i, "ok", i%2 == 0, uint8(id), pad)
		print("print ", id, " ", i, " ", pad, "\n")
		if i%50 == 0 {
			println("long", id, i, long, "end")
		}
		var p *int
		println("float", id, float64(i)/4, float32(id)/2, p)
	}
}

func main() {
	done := make(chan bool)
	for id := 0; id < tasks; id++ {
		task worker(id, done)
	}
	for id := 0; id < tasks; id++ {
		<-done
	}
	println("finished")
}
`

// TestPrintAtomic checks that print and println statements of concurrent
// tasks never mix within a line, with one processor and with all of them.
func TestPrintAtomic(t *testing.T) {
	testguard.Parallel(t)
	const (
		tasks = 8
		lines = 400
		pad   = "0123456789abcdefghijklmnopqrstuvwxyz"
	)
	long := strings.Repeat(pad, 17)
	re := map[string]*regexp.Regexp{
		"short":    regexp.MustCompile(`^short (\d) line (\d+) ok (true|false) (\d) ` + pad + `$`),
		"print":    regexp.MustCompile(`^print (\d) (\d+) ` + pad + `$`),
		"long":     regexp.MustCompile(`^long (\d) (\d+) ` + long + ` end$`),
		"float":    regexp.MustCompile(`^float (\d) ([0-9.]+) ([0-9.]+) 0x0$`),
		"deferred": regexp.MustCompile(`^deferred (\d) done$`),
		"finished": regexp.MustCompile(`^finished$`),
	}
	for _, level := range []int{0, 2} {
		prog := buildConc(t, printAtomicSrc, build.Config{OptLevel: level})
		for _, procs := range []string{"1", "4"} {
			t.Run(fmt.Sprintf("O%d/procs%s", level, procs), func(t *testing.T) {
				testguard.Parallel(t)
				got, status := runConc(t, prog, "PLUTO_MAXPROCS="+procs)
				if status != 0 {
					t.Fatalf("status %d, output:\n%s", status, got)
				}
				count := map[string]int{}
				for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
					kind, _, _ := strings.Cut(line, " ")
					r := re[kind]
					if r == nil || !r.MatchString(line) {
						t.Fatalf("mixed or unexpected line %q", line)
					}
					count[kind]++
				}
				want := map[string]int{
					"short":    tasks * lines,
					"print":    tasks * lines,
					"long":     tasks * lines / 50,
					"float":    tasks * lines,
					"deferred": tasks,
					"finished": 1,
				}
				for kind, n := range want {
					if count[kind] != n {
						t.Errorf("%d %s lines, want %d", count[kind], kind, n)
					}
				}
			})
		}
	}
}
