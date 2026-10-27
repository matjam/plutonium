package build

// The Go build runs external tools with os/exec (tool.go); this hand-written
// file runs them with pluto/os/exec, whose Cmd is an owner value and whose
// ExitError is a word (self-hosting plan, section 8).

import (
	"pluto/errors"
	"pluto/os"
	"pluto/os/exec"
	"compiler/compat"
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
func runTool(dir, name string, args []string) toolRun {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr compat.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stderr
	err := cmd.Run()
	var r toolRun
	if err != nil {
		r = toolRun{failed: true, notFound: errors.Is(err, exec.ErrNotFound), cause: compat.Concat(err.Error()), stderr: compat.Concat(stderr.String())}
	}
	cmd.Drop()
	return r
}
