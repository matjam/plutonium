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

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() *Config {
	return &Config{
		Target: "x86_64-linux",
		Arch:   "x86_64",
		OS:     "linux",
		Mode:   ModeHosted,
		Format: FormatELF,
		Tags:   map[string]bool{"amd64": true, "linux": true},
	}
}

// ParseTarget parses a target triple into Arch and OS.
func ParseTarget(triple string) (arch, os string) {
	parts := strings.SplitN(triple, "-", 2)
	if len(parts) == 1 {
		return parts[0], "linux"
	}
	return parts[0], parts[1]
}
