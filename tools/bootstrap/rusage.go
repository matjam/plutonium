package main

import (
	"os"
	"syscall"
)

// maxRSS returns the peak resident size of an exited process in MB, from
// its resource usage.
func maxRSS(st *os.ProcessState) (int, bool) {
	ru, ok := st.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0, false
	}
	return int(ru.Maxrss / 1024), true
}
