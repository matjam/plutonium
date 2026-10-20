package build

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
)

// A toolRun is the outcome of running an external tool.
type toolRun struct {
	failed   bool   // the tool could not be started or did not exit with status 0
	notFound bool   // the tool is not installed
	cause    string // why it failed: the error of starting or waiting for it
	stderr   string // what it wrote on standard error
}

// runTool runs the external tool name with args in directory dir ("" for
// the current one). What the tool writes on standard output goes to the
// compiler's standard error; its standard error is collected for the
// diagnostic of a failure.
//
// The self-hosted compiler replaces this file with tool.p, which runs the
// tool with pluto/os/exec (self-hosting plan, section 8).
func runTool(dir, name string, args []string) toolRun {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		return toolRun{failed: true, notFound: errors.Is(err, exec.ErrNotFound), cause: err.Error(), stderr: stderr.String()}
	}
	return toolRun{}
}
