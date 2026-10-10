//pu:build none && !kernel

package rt

// cpuNumber is the CPU number a panic reports: freestanding builds outside
// kernel mode have no per-CPU variables to keep one in, so it is 0.
func cpuNumber() int { return 0 }
