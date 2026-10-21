package testguard

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A proc is one process of /proc, as the watchdog sees it.
type proc struct {
	pid, ppid int
	rss       uint64 // bytes
	name      string // the command, from the stat line
}

// parseStat reads the pid, parent, command and resident size (field 24, in
// pages) of a /proc/<pid>/stat line. The command is in parentheses and may
// hold spaces and parentheses itself, so the fields after it are found from
// its last ')'.
func parseStat(s string, pageSize uint64) (proc, bool) {
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return proc{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:open]))
	if err != nil {
		return proc{}, false
	}
	// After the command: state ppid pgrp session tty_nr tpgid flags minflt
	// cminflt majflt cmajflt utime stime cutime cstime priority nice
	// num_threads itrealvalue starttime vsize rss ...
	f := strings.Fields(s[end+1:])
	if len(f) < 22 {
		return proc{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	pages, err2 := strconv.ParseUint(f[21], 10, 63)
	if err1 != nil || err2 != nil {
		return proc{}, false
	}
	return proc{pid: pid, ppid: ppid, rss: pages * pageSize, name: s[open+1 : end]}, true
}

// readProcs reads every process of the proc file system mounted at root.
func readProcs(root string) []proc {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	page := uint64(os.Getpagesize())
	var out []proc
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			continue // it exited
		}
		if p, ok := parseStat(string(b), page); ok {
			out = append(out, p)
		}
	}
	return out
}

// tree returns the process pid and its descendants among procs, pid first.
func tree(procs []proc, pid int) []proc {
	children := map[int][]proc{}
	var self *proc
	for i, p := range procs {
		children[p.ppid] = append(children[p.ppid], p)
		if p.pid == pid {
			self = &procs[i]
		}
	}
	if self == nil {
		return nil
	}
	out := []proc{*self}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i].pid]...)
	}
	return out
}

// runCgroup is the directory of the process's cgroup v2, or "" (set by
// Run).
var runCgroup string

// treeRSS returns the memory the test run uses, and the processes it
// started, largest first. That is the resident memory of this process and
// every process it started (the programs the tests build and run, go build,
// the linker): under a cgroup limit the children count as much as the test
// binary. If the run has its cgroup to itself (cgroup is its directory),
// it is the cgroup's usage if that is more, which also counts the files the
// tests write to a tmpfs and the kernel's memory.
func treeRSS(cgroup string) (uint64, []proc, bool) {
	self, ok := readRSS()
	if !ok {
		return 0, nil, false
	}
	procs := readProcs("/proc")
	t := tree(procs, os.Getpid())
	if len(t) == 0 {
		return self, nil, true
	}
	total := self
	kids := t[1:]
	for _, p := range kids {
		total += p.rss
	}
	sort.Slice(kids, func(i, j int) bool { return kids[i].rss > kids[j].rss })
	if cgroup != "" && dedicatedCgroup(cgroup, procs, os.Getpid()) {
		if used, ok := cgroupUsage(cgroup); ok {
			total = max(total, used)
		}
	}
	return total, kids, true
}

// describeProcs lists the largest processes of a tree for a report.
func describeProcs(kids []proc, n int) []string {
	var out []string
	for i, p := range kids {
		if i == n {
			out = append(out, fmt.Sprintf("... and %d more", len(kids)-i))
			break
		}
		out = append(out, fmt.Sprintf("%s (pid %d): %s", p.name, p.pid, mbString(p.rss)))
	}
	return out
}
