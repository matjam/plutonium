package config

import "strings"

// BuildMode is the compilation mode.
type BuildMode int

const (
	ModeHosted       BuildMode = iota // default: Linux hosted, syscalls available
	ModeFreestanding                  // freestanding: no OS, no syscalls
)

// OutputFormat is the final output format.
type OutputFormat int

const (
	FormatELF      OutputFormat = iota // ELF binary or object (default)
	FormatBinary                       // raw flat binary image
	FormatPE32Plus                     // PE32+ (UEFI)
)

// OptLevel is the optimization level.
type OptLevel int

const (
	OptNone OptLevel = iota
	OptO1
	OptO2
	OptSize
)

// Config holds the build configuration for a single compilation.
type Config struct {
	// Target
	Target string // e.g. "x86_64-linux", "aarch64-none"
	Arch   string // "x86_64", "aarch64"
	OS     string // "linux", "none"

	// Mode
	Mode BuildMode

	// Output
	OutputFile   string
	Format       OutputFormat
	LinkerScript string

	// Optimization
	Opt OptLevel

	// Build tags
	Tags map[string]bool

	// Flags
	Verbose bool
	EmitASM bool // -S: emit assembly instead of object files
}

// DefaultConfig returns a sensible default configuration for x86_64 Linux.
func DefaultConfig() *Config {
	cfg := &Config{
		Target: "x86_64-linux",
		Arch:   "x86_64",
		OS:     "linux",
		Mode:   ModeHosted,
		Format: FormatELF,
	}
	cfg.Tags = DeriveTagsFromTarget(cfg.Arch, cfg.OS)
	return cfg
}

// ParseTarget parses a target triple (e.g. "x86_64-linux", "aarch64-none")
// into the canonical arch and OS strings used internally.
//
// Arch is always the LLVM/triple form (x86_64, aarch64).
// The build tag form (amd64, arm64) is derived by ArchTag().
func ParseTarget(triple string) (arch, os string) {
	parts := strings.SplitN(triple, "-", 2)
	if len(parts) == 1 {
		return parts[0], "linux"
	}
	return parts[0], parts[1]
}

// ArchTag returns the build tag name for an arch triple component.
// LLVM triple names (x86_64, aarch64) are mapped to Go/Plutonium tag names
// (amd64, arm64). Unknown names are returned unchanged.
func ArchTag(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	case "i386", "i686":
		return "386"
	}
	return arch
}

// DeriveTagsFromTarget builds the complete set of active build tags for a
// given arch (LLVM triple form) and OS. It always includes:
//   - the arch tag (amd64, arm64, …)
//   - the OS tag (linux, none, …)
//   - "freestanding" when OS is "none" (bare metal implies freestanding)
func DeriveTagsFromTarget(arch, os string) map[string]bool {
	tags := map[string]bool{
		ArchTag(arch): true,
		os:            true,
	}
	if os == "none" {
		tags["freestanding"] = true
	}
	return tags
}
