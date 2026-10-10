package main

import (
	"os"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestMain lowers -parallel when the machine is short of memory and stops
// the run if the test binary grows past a cap (see internal/testguard).
func TestMain(m *testing.M) {
	os.Exit(testguard.Run(m, nil))
}
