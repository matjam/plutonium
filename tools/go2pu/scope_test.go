package main

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestScopeRewrites translates the testdata/scopes program and checks the
// rewrite of each form: the declaration, the tasks, the removed Add and Done
// calls, the parameters that stand for their arguments, and that no
// variable a task uses is allocated. TestDifferential builds and runs the
// translation.
func TestScopeRewrites(t *testing.T) {
	testguard.Parallel(t)
	res, err := TranslateDir(testConfig(), "testdata/scopes")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != 0 {
		t.Errorf("notes: %v", res.Notes)
	}
	src := string(res.Files[0].Src)
	for _, want := range []string{
		`"pluto/sync"`,
		"var wg sync.Scope",
		"wg.Task(func() {",
		"var mu sync.Mutex",
		"first.Task(func() {",
		"second.Wait()",
		"out[i] = work(i)\n",     // defer wg.Done() removed
		"out[i] = work(i) + 1\n", // wg.Done() removed
		"out[i] = 3 * i",         // go func(k int) {...}(i) with k replaced by i
		"out[i] = i + 100",       // go func(id int) {...}(i) with id replaced by i
		"mu.Lock()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("translation lacks %q:\n%s", want, src)
		}
	}
	code := withoutComments(src)
	for _, bad := range []string{"WaitGroup", ".Add(", ".Done(", "go func", ".Go(", "pu.New", "pu.Alloc", "(*"} {
		if strings.Contains(code, bad) {
			t.Errorf("translation still has %q:\n%s", bad, src)
		}
	}
	if got := strings.Count(src, "wg.Task("); got != 5 {
		t.Errorf("%d wg.Task calls, want 5", got)
	}
}

// TestScopeKept checks that a WaitGroup go2pu cannot rewrite exactly stays
// a WaitGroup, with a note that says why, and that the variables its
// goroutines use are allocated, as before.
func TestScopeKept(t *testing.T) {
	testguard.Parallel(t)
	res, err := TranslateDir(testConfig(), "testdata/scopekept")
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(res.Notes, "\n")
	for _, want := range []string{
		"sync.WaitGroup wg stays: a goroutine calls Done and keeps running (a latch)",
		"sync.WaitGroup wg stays: it is used other than by calling its methods",
		"sync.WaitGroup wg stays: the argument k is assigned after its declaration",
		"sync.WaitGroup wg stays: a goroutine returns before it calls Done",
		"sync.WaitGroup wg stays: a goroutine defers Done after other statements",
		"sync.WaitGroup wg stays: a goroutine is not preceded by a wg.Add statement",
		"sync.WaitGroup wg stays: no Wait call at the top level of its block",
		"sync.WaitGroup wg stays: Done is called in a function literal that is not started by a go statement of this function",
		"sync.WaitGroup wg stays: go2pu rewrites only a variable declared as var wg sync.WaitGroup",
		"sync.WaitGroup wg stays: Add has no goroutine",
		"sync.WaitGroup wg stays: the argument r is a named result, which return assigns",
		"sync.WaitGroup wg stays: the name i is redeclared where the goroutine uses id",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no note containing %q in:\n%s", want, all)
		}
	}
	// mixed is rewritten, so it has no note of its own (its line is the
	// last function).
	for _, n := range res.Notes {
		if strings.Contains(n, "stays") && strings.Contains(n, "mixed") {
			t.Errorf("note for the rewritten function: %s", n)
		}
	}
	src := string(res.Files[0].Src)
	for _, want := range []string{
		"wg := pu.Alloc[sync.WaitGroup]()", // a kept WaitGroup is allocated, as before
		"ready := pu.New(0)",               // so is what its goroutine uses
		"shared := pu.New(0)",              // the address goes to keepAddr, so mixed allocates it
		"total := 0",                       // only the task uses it: it stays in the frame
		"var wg sync.Scope",                // mixed is rewritten
		"wg.Task(func() {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("translation lacks %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, "total := pu.") {
		t.Errorf("total, which only the task uses, is allocated:\n%s", src)
	}
}

// withoutComments drops the lines of src that are comments.
func withoutComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}
