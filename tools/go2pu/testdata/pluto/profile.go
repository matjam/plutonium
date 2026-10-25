package main

// startProfiles starts the self-hosted compiler's profiles, which
// PLUTO_CPUPROFILE and PLUTO_MEMPROFILE ask for as they do of cmd/pluto,
// and returns the function that writes them. The translation replaces this
// file with the hand-written profile.p, which profiles with pluto/prof;
// this Go build of the driver does not profile itself (cmd/pluto's
// profiles cover the Go build of the compiler).
func startProfiles() func() {
	return func() {}
}
