package testguard

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cgroupRoot is where the cgroup v2 hierarchy is mounted.
const cgroupRoot = "/sys/fs/cgroup"

// cgroupLimit is what the cgroup v2 hierarchy leaves this process.
type cgroupLimit struct {
	// limit is the tightest memory.max or memory.high of the process's
	// cgroup and its ancestors, in bytes.
	limit uint64
	// avail is that limit's headroom: the least, over the cgroups with a
	// limit, of the limit minus what the cgroup uses (memory.current less
	// the file cache of memory.stat, which the kernel reclaims before it
	// kills anything).
	avail uint64
	// swap is memory.swap.max of the process's own cgroup in bytes, and
	// swapLimited whether it has one. Swap never counts as available: a
	// test run that swaps is as stuck as one that is killed.
	swap        uint64
	swapLimited bool
}

// cgroupMemory reads the limits of the process's cgroup. If the run has the
// cgroup to itself, what it already uses is the run's own and the watchdog
// counts it, so it is not taken off the headroom.
func cgroupMemory() (cgroupLimit, bool) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return cgroupLimit{}, false
	}
	cl, ok := readCgroup(string(b), cgroupRoot)
	if dir := selfCgroupDir(); ok && dir != "" && dedicatedCgroup(dir, readProcs("/proc"), os.Getpid()) {
		if used, ok := cgroupUsage(dir); ok {
			cl.avail = min(cl.limit, cl.avail+used)
		}
	}
	return cl, ok
}

// selfCgroupDir returns the directory of the process's cgroup v2, or "".
func selfCgroupDir() string {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	rel, ok := cgroupPath(string(b))
	if !ok {
		return ""
	}
	return filepath.Join(cgroupRoot, filepath.FromSlash(rel))
}

// readCgroup returns the limits of the cgroup v2 named in procCgroup (the
// text of /proc/self/cgroup) under the hierarchy mounted at root. It walks
// from the process's cgroup up to (not including) the root, whose files
// carry no limits; ok is false if no cgroup on the way has a memory limit.
func readCgroup(procCgroup, root string) (cl cgroupLimit, ok bool) {
	rel, found := cgroupPath(procCgroup)
	if !found {
		return cgroupLimit{}, false
	}
	leaf := filepath.Join(root, filepath.FromSlash(rel))
	if v, limited, ok := readLimit(filepath.Join(leaf, "memory.swap.max")); ok {
		cl.swap, cl.swapLimited = v, limited
	}
	for dir := leaf; ; dir = filepath.Dir(dir) {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			break
		}
		lim, limited := uint64(0), false
		for _, f := range []string{"memory.max", "memory.high"} {
			if v, l, ok := readLimit(filepath.Join(dir, f)); ok && l && (!limited || v < lim) {
				lim, limited = v, true
			}
		}
		if !limited {
			continue
		}
		used, _ := cgroupUsage(dir)
		head := lim - min(lim, used)
		if !ok || lim < cl.limit {
			cl.limit = lim
		}
		if !ok || head < cl.avail {
			cl.avail = head
		}
		ok = true
	}
	return cl, ok
}

// cgroupUsage returns what the cgroup at dir uses and cannot give back:
// memory.current less the file cache of memory.stat that the kernel can
// drop or write back before it kills anything. Files in a tmpfs (shmem, as
// /tmp often is) are file cache too, but without swap they stay, so they
// count as used.
func cgroupUsage(dir string) (uint64, bool) {
	used, limited, ok := readLimit(filepath.Join(dir, "memory.current"))
	if !ok || !limited {
		return 0, false
	}
	stat := filepath.Join(dir, "memory.stat")
	if file, ok := statField(stat, "file"); ok {
		shmem, _ := statField(stat, "shmem")
		used -= min(used, file-min(file, shmem))
	}
	return used, true
}

// dedicatedCgroup reports whether the cgroup at dir holds only this test
// run: the process (pid self), the processes it started, and the processes
// that started it (as bigjob.sh's systemd-run scope does). Its usage is then
// the run's, files in a tmpfs and kernel memory included.
func dedicatedCgroup(dir string, procs []proc, self int) bool {
	b, err := os.ReadFile(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		return false
	}
	ours := map[int]bool{}
	for _, p := range tree(procs, self) {
		ours[p.pid] = true
	}
	parent := map[int]int{}
	for _, p := range procs {
		parent[p.pid] = p.ppid
	}
	for pid := self; pid > 1; pid = parent[pid] {
		ours[pid] = true
		if _, known := parent[pid]; !known {
			break
		}
	}
	for _, f := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(f)
		if err != nil || !ours[pid] {
			return false
		}
	}
	return true
}

// cgroupPath returns the cgroup v2 path of the process: the "0::" line of
// /proc/self/cgroup.
func cgroupPath(procCgroup string) (string, bool) {
	for _, line := range strings.Split(procCgroup, "\n") {
		if rest, ok := strings.CutPrefix(line, "0::"); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// readLimit reads a cgroup file that holds a byte count or "max": limited is
// false for "max".
func readLimit(path string) (v uint64, limited, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false, false
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return 0, false, true
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false, false
	}
	return n, true, true
}

// statField returns a field of a memory.stat file.
func statField(path, name string) (uint64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, found := strings.Cut(sc.Text(), " ")
		if !found || k != name {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		return n, err == nil
	}
	return 0, false
}
