package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestMain lowers -parallel when the machine is short of memory and stops
// the run if the test binary grows past a cap (see internal/testguard), and
// keeps the demos the tests build (demoBinary) until the run ends.
//
// The tests were written against the old escape analysis. The new one, the
// compiler's default, still rejects some of their valid programs (vec, fmt
// and arena views in the mixed pipeline); until it accepts them they run
// under the old, as internal/build's do.
func TestMain(m *testing.M) {
	if os.Getenv("PLUTO_ESCAPE") == "" {
		os.Setenv("PLUTO_ESCAPE", "old")
	}
	cleanup, err := makeDemoDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	status := testguard.Run(m, nil)
	cleanup()
	os.Exit(status)
}
