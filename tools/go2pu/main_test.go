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
func TestMain(m *testing.M) {
	cleanup, err := makeDemoDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	status := testguard.Run(m, nil)
	cleanup()
	os.Exit(status)
}
