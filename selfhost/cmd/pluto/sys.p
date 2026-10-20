package main

// The driver's uses of the operating system that the library does
// differently from Go. The Go build has sys.go; this hand-written file runs
// the built program with pluto/os/exec, whose Cmd is an owner value and
// whose ExitError is a word, and keeps the listing file, an owner value, in
// the arena (self-hosting plan, section 8).

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/os/exec"
	"selfhost/internal/build"
	"selfhost/pu"
)

// runProgram runs the program at path with args, on the compiler's own
// standard streams, and returns its exit status.
func runProgram(path string, args []string) (int, error) {
	c := exec.Command(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	c.Drop()
	var ee exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}

// createListing creates the listing file of -S.
func createListing(name string) (io.Writer, error) {
	f := os.Create(name) or return nil, err
	return pu.New(f), nil
}

// closeListing closes the listing file of -S, if there is one.
func closeListing(cfg *build.Config) {
	if f, ok := cfg.Listing.(*os.File); ok && f != os.Stdout {
		_ = f.Close()
	}
}
