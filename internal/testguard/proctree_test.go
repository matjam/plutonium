package testguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseStat(t *testing.T) {
	// A command with a space and a parenthesis, as a test's program may have.
	const line = "4242 (pu (demo) x) S 4000 4242 4000 0 -1 4194304 100 0 0 0 5 3 0 0 20 0 7 0 12345 1000000 2560 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0\n"
	p, ok := parseStat(line, 4096)
	if !ok || p.pid != 4242 || p.ppid != 4000 || p.rss != 2560*4096 || p.name != "pu (demo) x" {
		t.Errorf("parseStat = %+v, %v", p, ok)
	}
	for _, bad := range []string{"", "12 (x", "x (y) S 1 2", "12 (x) S notapid 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21"} {
		if _, ok := parseStat(bad, 4096); ok {
			t.Errorf("parseStat(%q) succeeded", bad)
		}
	}
}

// TestTree checks that the tree of a process holds its descendants and
// nothing else.
func TestTree(t *testing.T) {
	procs := []proc{
		{pid: 1, ppid: 0}, {pid: 10, ppid: 1}, {pid: 11, ppid: 10}, {pid: 12, ppid: 11},
		{pid: 13, ppid: 10}, {pid: 20, ppid: 1}, {pid: 21, ppid: 20},
	}
	var pids []int
	for _, p := range tree(procs, 10) {
		pids = append(pids, p.pid)
	}
	if len(pids) != 4 || pids[0] != 10 || !containsInt(pids, 11) || !containsInt(pids, 12) || !containsInt(pids, 13) {
		t.Errorf("tree(10) = %v, want 10 then 11, 12 and 13", pids)
	}
	if got := tree(procs, 99); got != nil {
		t.Errorf("tree of a missing process = %v", got)
	}
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// TestReadProcs reads a fake /proc.
func TestReadProcs(t *testing.T) {
	root := t.TempDir()
	write := func(pid, stat string) {
		if err := os.MkdirAll(filepath.Join(root, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pid, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tail := " 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 "
	write("5", "5 (a) S 1"+tail+"10 0 0\n")
	write("6", "6 (b) S 5"+tail+"20 0 0\n")
	write("self", "ignored")
	write("7", "garbage")
	got := readProcs(root)
	if len(got) != 2 {
		t.Fatalf("readProcs = %+v", got)
	}
	page := uint64(os.Getpagesize())
	sum := uint64(0)
	for _, p := range tree(got, 5) {
		sum += p.rss
	}
	if sum != 30*page {
		t.Errorf("the tree of 5 holds %d pages", sum/page)
	}
}

// TestTreeRSS checks that a child process's memory counts: a child that
// holds memory raises the tree's total and is named in the list.
func TestTreeRSS(t *testing.T) {
	if _, _, ok := treeRSS(""); !ok {
		t.Skip("no /proc")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		self, _ := readRSS()
		total, kids, _ := treeRSS("")
		found := false
		for _, k := range kids {
			if k.pid == cmd.Process.Pid {
				found = true
				if total < self+k.rss {
					t.Errorf("total %d is less than the binary's %d and the child's %d", total, self, k.rss)
				}
			}
		}
		if found {
			r := report(total, 1, kids)
			if !strings.Contains(r, "sleep (pid") {
				t.Errorf("the report does not name the child:\n%s", r)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child %d is not in the tree: %+v", cmd.Process.Pid, kids)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
