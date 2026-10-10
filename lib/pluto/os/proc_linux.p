package os

import (
	"pluto/errors"
	"pluto/syscall"
)

// A Signal is a Unix signal number.
type Signal int

// Signals every Unix system has.
const (
	Interrupt Signal = syscall.SIGINT
	Kill      Signal = syscall.SIGKILL
)

// A Process is a child process started by pluto/os/exec.
type Process struct {
	Pid  int
	done bool
}

// Signal sends sig to the process.
func (p *Process) Signal(sig Signal) error {
	if p.done {
		return ErrProcessDone
	}
	return syscall.Kill(p.Pid, int(sig))
}

// Kill makes the process exit at once (SIGKILL).
func (p *Process) Kill() error { return p.Signal(Kill) }

// Wait waits for the process to exit and returns its state. A process can
// be waited for once.
func (p *Process) Wait() (ProcessState, error) {
	if p.done {
		return ProcessState{}, ErrProcessDone
	}
	var status int32
	for {
		_, err := syscall.Wait4(p.Pid, &status, 0)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return ProcessState{}, err
		}
		break
	}
	p.done = true
	return ProcessState{pid: p.Pid, status: syscall.WaitStatus(status)}, nil
}

// ErrProcessDone is reported for a process that has already been waited
// for.
var ErrProcessDone = errors.New("os: process already finished")

// A ProcessState describes a process that has exited.
type ProcessState struct {
	pid    int
	status syscall.WaitStatus
}

// Pid returns the process ID of the exited process.
func (p ProcessState) Pid() int { return p.pid }

// Exited reports whether the process exited on its own (rather than by a
// signal).
func (p ProcessState) Exited() bool { return p.status.Exited() }

// Success reports whether the process exited with status 0.
func (p ProcessState) Success() bool { return p.status.Exited() && p.status.ExitStatus() == 0 }

// ExitCode returns the exit status, or -1 if a signal ended the process.
func (p ProcessState) ExitCode() int { return p.status.ExitStatus() }

// Sys returns the raw wait status.
func (p ProcessState) Sys() syscall.WaitStatus { return p.status }
