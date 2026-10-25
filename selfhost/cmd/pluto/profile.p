package main

// The self-hosted compiler's profiles, which the environment asks for as it
// does of cmd/pluto (cmd/pluto/profile.go): PLUTO_CPUPROFILE=file writes a
// CPU profile and PLUTO_MEMPROFILE=file an allocation profile, both in the
// pprof format, when the compiler finishes. The Go build has profile.go,
// which does nothing; this hand-written file replaces it in the
// translation.
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
)

var cpuProfile, memProfile string

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
	return stopProfiles
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
