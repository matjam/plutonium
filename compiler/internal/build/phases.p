package build

import (
	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
	"compiler/internal/load"
	"pluto/bytes"
	"pluto/os"
	"pluto/time"
)

// phases says to name each phase of a build on standard error as it
// starts, and as it ends to say how long it took and the most memory the
// compiler held during it, so that a memory or time profile taken from
// outside can be matched to the phases:
//
//	pluto: phase lower
//	pluto: phase lower done 41250ms peak 5120MB
//
// The load phase also gives the time spent parsing and type-checking, and
// the opt phase the time of each pass, added up over every function.
var phases = compat.Getenv("PLUTO_PHASES") != ""

func init() {
	if phases {
		load.Timing = true
		solve.Measure = measureSolve
	}
}

// The escape analysis's solves, for PLUTO_PHASES: how many, their steps
// (constraint firings and edge transforms) added up, and the solves of
// at least bigSolve steps, each named as it ends.
var (
	solves, solveSteps, bigSolves int64
)

const bigSolve = 1000000

func measureSolve(f *eir.Func, m solve.Measurement) {
	solves++
	solveSteps += int64(m.Steps)
	if m.Steps >= bigSolve {
		bigSolves++
		compat.Fprintfb(os.Stderr, "pluto: escape solve %v: %v steps, %v tokens, %v nodes, %vKB\n",
			compat.ArgS(f.Name), compat.ArgI(int64(m.Steps)), compat.ArgI(int64(m.Tokens)),
			compat.ArgI(int64(m.Nodes)), compat.ArgI(int64(m.ArenaBytes/1024)))
	}
}

// PhaseHook, when set, is called as each phase starts, after it is named.
// The self-hosted driver uses it to write its profiles and stop at the
// phase PLUTO_PROFILE_PHASE names.
var PhaseHook func(name string)

// The phase running, for PLUTO_PHASES, and when it started.
var (
	phaseName  string
	phaseStart time.Time
)

func phase(name string) {
	if phases {
		endPhase()
		compat.Fprintfb(os.Stderr, "%v %v\n", compat.ArgS("pluto: phase"), compat.ArgS(name))
		phaseName = name
		resetPeak()
		phaseStart = time.Now()
	}
	if PhaseHook != nil {
		PhaseHook(name)
	}
}

// endPhase reports the end of the phase running, if any, for PLUTO_PHASES.
func endPhase() {
	if !phases || phaseName == "" {
		return
	}
	ms := time.Since(phaseStart).Milliseconds()
	compat.Fprintfb(os.Stderr, "pluto: phase %v done %vms peak %vMB\n",
		compat.ArgS(phaseName), compat.ArgI(ms), compat.ArgI(peakKB()/1024))
	if phaseName == "escape" {
		compat.Fprintfb(os.Stderr, "pluto: phase escape: %v solves, %v steps, %v solves of %v steps or more\n",
			compat.ArgI(solves), compat.ArgI(solveSteps), compat.ArgI(bigSolves), compat.ArgI(bigSolve))
	}
	if phaseName == "load" {
		compat.Fprintfb(os.Stderr, "pluto: phase load: parse %vms, types2 %vms\n",
			compat.ArgI(load.ParseTime.Milliseconds()), compat.ArgI(load.CheckTime.Milliseconds()))
	}
	phaseName = ""
}

// passTime reports the time an optimiser pass took, for PLUTO_PHASES.
func passTime(name string, d time.Duration) {
	compat.Fprintfb(os.Stderr, "pluto: opt pass %v %vms\n", compat.ArgS(name), compat.ArgI(d.Milliseconds()))
}

// resetPeak sets the kernel's record of the process's peak resident memory
// to what it holds now (Linux's clear_refs), so that the peak read at the
// end of a phase is that phase's.
func resetPeak() {
	_ = os.WriteFile("/proc/self/clear_refs", compat.Bytes("5"), 0)
}

// peakKB returns the process's peak resident memory in kilobytes (VmHWM in
// /proc/self/status), or 0 if it cannot be read.
func peakKB() int64 {
	data, err := compat.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	i := bytes.Index(data, compat.Bytes("VmHWM:"))
	if i < 0 {
		return 0
	}
	var kb int64
	for _, c := range data[i+len("VmHWM:"):] {
		if c >= '0' && c <= '9' {
			kb = kb*10 + int64(c-'0')
		} else if c != ' ' && c != '\t' {
			break
		}
	}
	return kb
}
