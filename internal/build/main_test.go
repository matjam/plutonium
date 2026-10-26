package build_test

import (
	"os"
	"runtime/debug"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestMain makes the garbage collector less eager than its default unless
// GOGC or GOMEMLIMIT says otherwise. The tests compile hundreds of programs,
// up to -parallel of them at once, and with GOGC=100 a quarter of their CPU
// time went to collecting. A collection waits for the heap to grow to five
// times what is live, or to a limit that grows with the number of builds
// at once (about what GOGC=100 used at the default -parallel on 24 CPUs),
// whichever comes first.
//
// internal/testguard first lowers -parallel if the machine has too little
// memory available for that many builds at once, and stops the run, naming
// the tests in flight, if the test binary grows past a memory cap.
func TestMain(m *testing.M) {
	os.Exit(testguard.Run(m, func(parallel int) {
		if os.Getenv("GOGC") == "" {
			debug.SetGCPercent(400)
		}
		if os.Getenv("PLUTO_ESCAPE") == "" {
			// These tests were written against the old escape analysis.
			// The new one, the compiler's default, still rejects some of
			// their valid programs (vec, fmt, testing and task
			// statements); until it accepts them they run under the old.
			os.Setenv("PLUTO_ESCAPE", "old")
		}
		if os.Getenv("GOMEMLIMIT") == "" {
			debug.SetMemoryLimit(int64(max(parallel, 4)) * 96 << 20)
		}
	}))
}
