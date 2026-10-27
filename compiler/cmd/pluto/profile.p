package main

// The self-hosted compiler's profiles, which the environment asks for as it
// does of cmd/pluto (cmd/pluto/profile.go): PLUTO_CPUPROFILE=file writes a
// CPU profile and PLUTO_MEMPROFILE=file an allocation profile, both in the
// pprof format, when the compiler finishes. The Go build has profile.go,
// which does nothing; this hand-written file replaces it in the
// translation.
//
// PLUTO_PROFILE_PHASE=name writes the profiles and stops the build as the
// phase of that name starts (the names PLUTO_PHASES prints), for a profile
// of the phases before it.
//
// The allocation profile sees what is allocated through the default
// allocator; the scratch arenas of the analyses show as the arenas' own
// requests for memory. The compiler's translated code does not free, so
// the bytes allocated are close to the memory it holds. It needs a build
// without generation checks (--gencheck=false): with them, the memory the
// sampler passes on would be registered twice, by the heap and by it.

import (
	"pluto/os"
	"pluto/prof"
	"compiler/internal/build"
)

var cpuProfile, memProfile, stopPhase string

// startProfiles starts the profiles the environment asks for, and returns
// the function that stops them and writes them.
func startProfiles() func() {
	cpuProfile = os.GetenvLossy("PLUTO_CPUPROFILE")
	memProfile = os.GetenvLossy("PLUTO_MEMPROFILE")
	if cpuProfile != "" {
		if err := prof.StartCPUProfile(); err != nil {
			println("pluto:", err.Error())
			cpuProfile = ""
		}
	}
	if memProfile != "" {
		if err := prof.StartAllocProfile(0); err != nil {
			println("pluto:", err.Error())
			memProfile = ""
		}
	}
	stopPhase = os.GetenvLossy("PLUTO_PROFILE_PHASE")
	if stopPhase != "" {
		build.PhaseHook = atPhase
	}
	return stopProfiles
}

// atPhase writes the profiles and exits when the phase PLUTO_PROFILE_PHASE
// names starts.
func atPhase(name string) {
	if name == stopPhase {
		stopProfiles()
		os.Exit(130)
	}
}

func stopProfiles() {
	if cpuProfile != "" {
		if err := prof.StopCPUProfile(cpuProfile); err != nil {
			println("pluto: writing the CPU profile:", err.Error())
		}
	}
	if memProfile != "" {
		if err := prof.StopAllocProfile(memProfile); err != nil {
			println("pluto: writing the allocation profile:", err.Error())
		}
	}
}
