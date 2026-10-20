package main

import (
	"io"
	"os"
	"os/exec"

	"github.com/matjam/plutonium/internal/build"
)

// The driver's uses of the operating system that the library does
// differently: the translation replaces this file with sys.p, which uses
// pluto/os/exec and pluto/os.

// runProgram runs the program at path with args, on the compiler's own
// standard streams, and returns its exit status.
func runProgram(path string, args []string) (int, error) {
	c := exec.Command(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 0, err
}

// createListing creates the listing file of -S.
func createListing(name string) (io.Writer, error) {
	return os.Create(name)
}

// closeListing closes the listing file of -S, if there is one.
func closeListing(cfg *build.Config) {
	if f, ok := cfg.Listing.(*os.File); ok && f != os.Stdout {
		_ = f.Close()
	}
}
